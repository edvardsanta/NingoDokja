FROM node:22-bookworm

RUN corepack enable

WORKDIR /app

COPY dokja_services/dokja_whatsapp/package.json /app/package.json
COPY dokja_services/dokja_whatsapp/pnpm-lock.yaml /app/pnpm-lock.yaml
COPY dokja_services/dokja_whatsapp/pnpm-workspace.yaml /app/pnpm-workspace.yaml

RUN pnpm install --frozen-lockfile

COPY dokja_services/dokja_whatsapp /app

# No port: the service connects out to WhatsApp and to the orchestrator, and listens on nothing.
CMD ["pnpm", "start"]
