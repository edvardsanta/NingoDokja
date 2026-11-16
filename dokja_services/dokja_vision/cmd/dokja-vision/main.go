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
	options := []vision.Option{
		vision.WithModel(env("DOKJA_VISION_MODEL", "minicpm-v")),
		vision.WithMaxBytes(maxMiB << 20),
	}
	// An operator-written prompt overrides the built-in one without a
	// rebuild, the same way visions.md tunes Ningo's worldview.
	if promptFile := env("DOKJA_VISION_PROMPT_FILE", ""); promptFile != "" {
		if prompt, err := vision.PromptFromFile(promptFile); err == nil {
			options = append(options, vision.WithPrompt(prompt))
			log.Printf("dokja-vision prompt loaded from %s", promptFile)
		} else {
			log.Printf("dokja-vision: %v; keeping the built-in prompt", err)
		}
	}
	service := vision.NewService(ollama, options...)
	server := vision.NewServer(service, env("DOKJA_VISION_HOST", "0.0.0.0"), env("DOKJA_VISION_PORT", "8082"))
	log.Printf("dokja-vision listening on %s model=%s ollama=%s", server.Addr(), service.Model(), ollama)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
