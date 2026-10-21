// The runtime daemon is host infrastructure, not a Compose service: it must be
// available when all application containers are stopped.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	domain "read_books/dokja_domain/dokja_runtime"
	orchestration "read_books/dokja_orch/runtime"
	service "read_books/dokja_services/dokja_runtime"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	rootFlag := flag.String("root", "", "local repository root (lite stack only)")
	stateFlag := flag.String("state-dir", "", "private daemon state directory")
	flag.Parse()
	root, err := filepath.EvalSymlinks(*rootFlag)
	if err != nil || !filepath.IsAbs(root) {
		return fmt.Errorf("a local absolute repository root is required")
	}
	if info, err := os.Stat(filepath.Join(root, "docker-compose.lite.yml")); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("lite compose configuration not found")
	}
	state := *stateFlag
	if !filepath.IsAbs(state) {
		return fmt.Errorf("private absolute state directory is required")
	}
	if err := os.MkdirAll(state, 0700); err != nil {
		return err
	}
	if err := os.Chmod(state, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(state, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return fmt.Errorf("runtime daemon already running")
	}
	socket := filepath.Join(state, "control.sock")
	// Holding the OS lock makes stale-socket cleanup safe across concurrent launches.
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to replace a non-socket control path")
		}
		if err := os.Remove(socket); err != nil {
			return err
		}
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socket)
	if err := os.Chmod(socket, 0600); err != nil {
		return err
	}
	path := filepath.Join(state, "ownership.json")
	journal := domain.Journal{}
	raw, err := os.ReadFile(path)
	if err == nil {
		if len(raw) > 16384 || json.Unmarshal(raw, &journal) != nil {
			return fmt.Errorf("invalid ownership journal; inspect private runtime state before restarting")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	save := func(journal domain.Journal) error {
		raw, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		temp := path + ".tmp"
		if err := os.WriteFile(temp, raw, 0600); err != nil {
			return err
		}
		return os.Rename(temp, path)
	}
	d := domain.New(service.New(root), journal, save)
	hash := sha256.Sum256([]byte(root))
	project := hex.EncodeToString(hash[:])[:16]
	server := &http.Server{Handler: orchestration.Handler(d, project), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 15 * time.Second}
	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-signals.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
