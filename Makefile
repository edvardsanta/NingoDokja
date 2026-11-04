.DEFAULT_GOAL := help

COMPOSE ?= docker compose
PNPM ?= pnpm
ENV_FILE ?= .env
STACK_FILE ?= docker-compose.lite.yml
# Explicit services avoid starting the interactive CLI/TUI containers.
SERVICES ?= dokja-orchestrator dokja-discord dokja-feeds dokja-memory dokja-knowledge dokja-voice dokja-scheduler
COMPOSE_CMD = $(COMPOSE) --env-file "$(ENV_FILE)" -f "$(STACK_FILE)" -f docker-compose.postgres.yml

.PHONY: help start desktop desktop-install up status logs

help:
	@echo "make start           Start the local stack, then open Desktop"
	@echo "make desktop         Build and open Desktop only"
	@echo "make desktop-install Install Desktop dependencies"
	@echo "make up              Start local services, including PostgreSQL"
	@echo "make status          Show container states"
	@echo "make logs            Follow application service logs"

# Keep startup ordered even when make is invoked with -j.
start: up
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
	$(COMPOSE_CMD) up -d $(SERVICES)

status:
	$(COMPOSE_CMD) ps

logs:
	$(COMPOSE_CMD) logs --tail=100 -f $(SERVICES)
