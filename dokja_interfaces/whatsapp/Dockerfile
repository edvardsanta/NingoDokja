FROM node:22-bookworm

RUN corepack enable

WORKDIR /app

COPY dokja_interfaces/whatsapp/package.json /app/package.json
COPY dokja_interfaces/whatsapp/pnpm-lock.yaml /app/pnpm-lock.yaml
COPY dokja_interfaces/whatsapp/pnpm-workspace.yaml /app/pnpm-workspace.yaml

RUN pnpm install --frozen-lockfile

COPY dokja_interfaces/whatsapp /app

# No port: the service connects out to WhatsApp and to the orchestrator, and listens on nothing.
CMD ["pnpm", "start"]
