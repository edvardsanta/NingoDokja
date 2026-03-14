FROM node:22-bookworm

RUN corepack enable
RUN apt-get update && apt-get install -y ffmpeg && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY dokja_interfaces/discord/package.json /app/package.json
COPY dokja_interfaces/discord/pnpm-lock.yaml /app/pnpm-lock.yaml

RUN pnpm install --frozen-lockfile

COPY dokja_interfaces/discord /app

EXPOSE 8092

CMD ["pnpm", "dev"]
