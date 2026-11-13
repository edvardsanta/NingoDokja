.DEFAULT_GOAL := help

COMPOSE ?= docker compose
PNPM ?= pnpm
ENV_FILE ?= .env
STACK_FILE ?= docker-compose.lite.yml
# Explicit services avoid starting the interactive CLI/TUI containers.
SERVICES ?= dokja-orchestrator dokja-discord dokja-feeds dokja-memory dokja-knowledge dokja-scheduler
# dokja-voice is opt-in: its image is large (the ROCm build alone needs several GB), so nothing
# builds or starts it unless VOICE=1 is given or `make voice` is run.
VOICE ?= 0
VOICE_ON := $(filter 1,$(VOICE))
# GPU transcription (docker-compose.voice-gpu.yml) applies only with VOICE=1, and then whenever
# ENV_FILE names a Whisper model in DOKJA_VOICE_CPP_MODEL_FILE; without it, dokja-voice falls back
# to its CPU provider. VOICE_GPU=0 turns the overlay off; VOICE_GPU=1 requires it.
VOICE_GPU ?= auto
VOICE_GPU_MODEL := $(shell sed -n 's/^DOKJA_VOICE_CPP_MODEL_FILE=//p' "$(ENV_FILE)" 2>/dev/null | tail -n 1 | tr -d "\"'")
VOICE_GPU_ON := $(if $(VOICE_ON),$(if $(filter 1,$(VOICE_GPU)),1,$(if $(filter auto,$(VOICE_GPU)),$(if $(VOICE_GPU_MODEL),1,),)),)
VOICE_GPU_FILES := $(if $(VOICE_GPU_ON),-f docker-compose.voice-gpu.yml,)
# The local Ollama on the GPU (docker-compose.ollama-gpu.yml) is on whenever ENV_FILE sets
# DOKJA_OLLAMA_RENDER_DEVICE; without it Ollama runs on the CPU. OLLAMA_GPU=0 turns the overlay off;
# OLLAMA_GPU=1 requires it, with the default render node.
OLLAMA_GPU ?= auto
OLLAMA_GPU_DEVICE := $(shell sed -n 's/^DOKJA_OLLAMA_RENDER_DEVICE=//p' "$(ENV_FILE)" 2>/dev/null | tail -n 1 | tr -d "\"'")
OLLAMA_GPU_ON := $(if $(filter 1,$(OLLAMA_GPU)),1,$(if $(filter auto,$(OLLAMA_GPU)),$(if $(OLLAMA_GPU_DEVICE),1,),))
OLLAMA_GPU_FILES := $(if $(OLLAMA_GPU_ON),-f docker-compose.ollama-gpu.yml,)
VOICE_SERVICES = dokja-voice $(if $(VOICE_GPU_ON),dokja-whisper-gpu,)
RUN_SERVICES = $(SERVICES) $(if $(VOICE_ON),$(VOICE_SERVICES),) $(if $(OLLAMA_GPU_ON),dokja-ollama,)
COMPOSE_CMD = $(COMPOSE) --env-file "$(ENV_FILE)" -f "$(STACK_FILE)" -f docker-compose.postgres.yml $(VOICE_GPU_FILES) $(OLLAMA_GPU_FILES)

.PHONY: help start desktop desktop-install up voice status logs backup

help:
	@echo "make start           Start/restart local services, then open Desktop"
	@echo "make desktop         Build and open Desktop only"
	@echo "make desktop-install Install Desktop dependencies"
	@echo "make up              Start local services, including PostgreSQL"
	@echo "make status          Show container states"
	@echo "make logs            Follow application service logs"
	@echo "make backup          Dump PostgreSQL to .dokja/backups/postgres (KEEP=14 dumps)"
	@echo "make voice           Build and start only dokja-voice (opt-in; large image)"
	@echo "Voice is off by default: add VOICE=1 to make start/up/status/logs to include it"
	@echo "GPU transcription: with VOICE=1, on when ENV_FILE sets DOKJA_VOICE_CPP_MODEL_FILE (VOICE_GPU=0 to disable)"
	@echo "GPU for local Ollama: on when ENV_FILE sets DOKJA_OLLAMA_RENDER_DEVICE (OLLAMA_GPU=0 to disable)"

# Keep startup ordered even when make is invoked with -j.
start: up
	$(COMPOSE_CMD) restart $(RUN_SERVICES)
	$(MAKE) desktop

desktop:
	@if [ ! -d dokja_interfaces/desktop/node_modules ]; then $(MAKE) desktop-install; fi
	$(PNPM) --dir dokja_interfaces/desktop start --env-file="$(abspath $(ENV_FILE))"

desktop-install:
	$(PNPM) --dir dokja_interfaces/desktop install --frozen-lockfile

up:
	$(COMPOSE_CMD) up -d dokja-postgres
	@attempt=0; until $(COMPOSE_CMD) exec -T dokja-postgres pg_isready -U dokja -d dokja >/dev/null 2>&1; do \
		attempt=$$((attempt + 1)); \
		if [ "$$attempt" -ge 30 ]; then \
			echo "PostgreSQL is not ready. Check: make logs SERVICES=dokja-postgres" >&2; exit 1; \
		fi; \
		sleep 1; \
	done
	$(COMPOSE_CMD) up -d $(RUN_SERVICES)

# Starts only the voice service(s), without their dependencies or the rest of the stack.
voice:
	$(MAKE) --no-print-directory voice-up VOICE=1

.PHONY: voice-up
voice-up:
	$(COMPOSE_CMD) up -d --no-deps --build $(VOICE_SERVICES)

status:
	$(COMPOSE_CMD) ps

logs:
	$(COMPOSE_CMD) logs --tail=100 -f $(SERVICES)

# The dump keeps the newest KEEP files in .dokja/backups/postgres. The compose command is passed
# without its double quotes because the script splits it into words; keep paths free of spaces.
backup:
	COMPOSE_CMD='$(subst ",,$(COMPOSE_CMD))' $(if $(KEEP),KEEP=$(KEEP) ,)scripts/backup-postgres.sh
