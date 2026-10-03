//go:build unix

package feeds

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const testPath = "PATH=/usr/local/bin:/usr/bin:/bin"

// scriptPlugin writes run.sh into a new directory and returns a plugin that runs it.
func scriptPlugin(t *testing.T, body string, timeout time.Duration) Plugin {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "run.sh")
	writeFile(t, path, "#!/bin/sh\n"+body+"\n", 0o755)
	return Plugin{ID: "test", Name: "Test", Dir: dir, Command: []string{path}, Timeout: timeout, MaxItems: 10}
}

func waitUntil(t *testing.T, limit time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the condition was not met in time")
}

// processAlive is false for a process that is gone, and for a zombie, which is dead but not
// yet collected by whoever adopted it.
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	if stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		if i := bytes.LastIndexByte(stat, ')'); i >= 0 && i+2 < len(stat) && stat[i+2] == 'Z' {
			return false
		}
	}
	return true
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the plugin did not write its child's pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func TestRunReturnsWhatThePluginPrinted(t *testing.T) {
	plugin := scriptPlugin(t, `echo '{"items":[]}'`, 5*time.Second)
	out, err := ExecRunner{}.Run(context.Background(), plugin, []string{testPath})
	if err != nil || strings.TrimSpace(string(out)) != `{"items":[]}` {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestRunStartsThePluginInItsOwnDirectory(t *testing.T) {
	plugin := scriptPlugin(t, `pwd`, 5*time.Second)
	out, err := ExecRunner{}.Run(context.Background(), plugin, []string{testPath})
	want, _ := filepath.EvalSymlinks(plugin.Dir)
	if err != nil || strings.TrimSpace(string(out)) != want {
		t.Fatalf("out=%q want=%q err=%v", out, want, err)
	}
}

func TestRunReportsAFailureWithoutPuttingStderrInTheError(t *testing.T) {
	plugin := scriptPlugin(t, `echo "token=SECRET-VALUE" >&2; exit 3`, 5*time.Second)
	var logs bytes.Buffer
	_, err := ExecRunner{Logger: log.New(&logs, "", 0)}.Run(context.Background(), plugin, []string{testPath})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), "SECRET-VALUE") {
		t.Fatalf("stderr reached the error that is shown: %v", err)
	}
	if !strings.Contains(logs.String(), "SECRET-VALUE") {
		t.Fatalf("stderr did not reach the log, so the owner cannot debug the plugin: %q", logs.String())
	}
}

func TestRunStopsAPluginThatTakesTooLong(t *testing.T) {
	plugin := scriptPlugin(t, `sleep 30`, 300*time.Millisecond)
	started := time.Now()
	_, err := ExecRunner{}.Run(context.Background(), plugin, []string{testPath})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the plugin was stopped after %v", elapsed)
	}
}

func TestRunKillsEverythingThePluginStarted(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	plugin := scriptPlugin(t, "sleep 60 &\necho $! > "+pidFile+"\nwait", time.Second)

	_, err := ExecRunner{}.Run(context.Background(), plugin, []string{testPath})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err=%v", err)
	}
	pid := readPID(t, pidFile)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	waitUntil(t, 5*time.Second, func() bool { return !processAlive(pid) })
}

func TestRunDoesNotWaitForAChildThatKeepsThePipeOpen(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	plugin := scriptPlugin(t, "sleep 60 &\necho $! > "+pidFile+"\necho '{\"items\":[]}'", 20*time.Second)

	started := time.Now()
	out, err := ExecRunner{}.Run(context.Background(), plugin, []string{testPath})
	if err != nil || !strings.Contains(string(out), `"items"`) {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("the run took %v", elapsed)
	}
	pid := readPID(t, pidFile)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	waitUntil(t, 5*time.Second, func() bool { return !processAlive(pid) })
}

func TestRunStopsAPluginThatPrintsTooMuch(t *testing.T) {
	plugin := scriptPlugin(t, `yes x`, 20*time.Second)
	started := time.Now()
	_, err := ExecRunner{MaxOutput: 2048}.Run(context.Background(), plugin, []string{testPath})
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("the plugin kept running for %v", elapsed)
	}
}

func TestRunAcceptsOutputExactlyAtTheLimit(t *testing.T) {
	plugin := scriptPlugin(t, `printf '%0100d' 0`, 5*time.Second)
	out, err := ExecRunner{MaxOutput: 100}.Run(context.Background(), plugin, []string{testPath})
	if err != nil || len(out) != 100 {
		t.Fatalf("len=%d err=%v", len(out), err)
	}
}

func TestRunGivesThePluginOnlyTheEnvironmentItIsHanded(t *testing.T) {
	t.Setenv("FEEDS_TEST_SECRET", "must-not-leak")
	plugin := scriptPlugin(t, `printf '%s|%s' "${FEEDS_TEST_SECRET-unset}" "${DECLARED-unset}"`, 5*time.Second)
	out, err := ExecRunner{}.Run(context.Background(), plugin, []string{testPath, "DECLARED=yes"})
	if err != nil || string(out) != "unset|yes" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestRunStopsWhenTheServiceStops(t *testing.T) {
	plugin := scriptPlugin(t, `sleep 30`, 20*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	started := time.Now()
	_, err := ExecRunner{}.Run(ctx, plugin, []string{testPath})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the run took %v to stop", elapsed)
	}
}
