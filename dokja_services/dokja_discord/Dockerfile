FROM node:22-bookworm

RUN corepack enable
RUN apt-get update && apt-get install -y ffmpeg && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY dokja_services/dokja_discord/package.json /app/package.json
COPY dokja_services/dokja_discord/pnpm-lock.yaml /app/pnpm-lock.yaml
COPY dokja_services/dokja_discord/pnpm-workspace.yaml /app/pnpm-workspace.yaml

RUN pnpm install --frozen-lockfile

COPY dokja_services/dokja_discord /app

EXPOSE 8092

CMD ["pnpm", "start"]
