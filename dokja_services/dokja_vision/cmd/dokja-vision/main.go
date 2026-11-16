package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"dokja_services/dokja_vision/internal/vision"
)

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func main() {
	maxMiB, err := strconv.ParseInt(env("DOKJA_VISION_MAX_MIB", "10"), 10, 64)
	if err != nil || maxMiB < 1 || maxMiB > 50 {
		log.Fatalf("invalid DOKJA_VISION_MAX_MIB")
	}
	ollama := env("DOKJA_VISION_OLLAMA", "http://dokja-ollama:11434")
	service := vision.NewService(
		ollama,
		vision.WithModel(env("DOKJA_VISION_MODEL", "minicpm-v")),
		vision.WithMaxBytes(maxMiB<<20),
	)
	server := vision.NewServer(service, env("DOKJA_VISION_HOST", "0.0.0.0"), env("DOKJA_VISION_PORT", "8082"))
	log.Printf("dokja-vision listening on %s model=%s ollama=%s", server.Addr(), service.Model(), ollama)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
