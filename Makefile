.DEFAULT_GOAL := help

COMPOSE ?= docker compose
PNPM ?= pnpm
ENV_FILE ?= .env
STACK_FILE ?= docker-compose.lite.yml
# Explicit services avoid starting the interactive CLI/TUI containers.
SERVICES ?= dokja-orchestrator dokja-discord dokja-feeds dokja-memory dokja-knowledge dokja-voice dokja-scheduler
# GPU transcription (docker-compose.voice-gpu.yml) is on whenever ENV_FILE names a Whisper model
# in DOKJA_VOICE_CPP_MODEL_FILE; without it, dokja-voice falls back to its CPU provider.
# VOICE_GPU=0 turns the overlay off; VOICE_GPU=1 requires it.
VOICE_GPU ?= auto
VOICE_GPU_MODEL := $(shell sed -n 's/^DOKJA_VOICE_CPP_MODEL_FILE=//p' "$(ENV_FILE)" 2>/dev/null | tail -n 1 | tr -d "\"'")
VOICE_GPU_ON := $(if $(filter 1,$(VOICE_GPU)),1,$(if $(filter auto,$(VOICE_GPU)),$(if $(VOICE_GPU_MODEL),1,),))
VOICE_GPU_FILES := $(if $(VOICE_GPU_ON),-f docker-compose.voice-gpu.yml,)
RUN_SERVICES = $(SERVICES) $(if $(VOICE_GPU_ON),dokja-whisper-gpu,)
COMPOSE_CMD = $(COMPOSE) --env-file "$(ENV_FILE)" -f "$(STACK_FILE)" -f docker-compose.postgres.yml $(VOICE_GPU_FILES)

.PHONY: help start desktop desktop-install up status logs

help:
	@echo "make start           Start/restart local services, then open Desktop"
	@echo "make desktop         Build and open Desktop only"
	@echo "make desktop-install Install Desktop dependencies"
	@echo "make up              Start local services, including PostgreSQL"
	@echo "make status          Show container states"
	@echo "make logs            Follow application service logs"
	@echo "GPU transcription: on when ENV_FILE sets DOKJA_VOICE_CPP_MODEL_FILE (VOICE_GPU=0 to disable)"

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

status:
	$(COMPOSE_CMD) ps

logs:
	$(COMPOSE_CMD) logs --tail=100 -f $(SERVICES)
