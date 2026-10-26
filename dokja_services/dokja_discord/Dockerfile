FROM node:22-bookworm

RUN corepack enable
RUN apt-get update && apt-get install -y ffmpeg python3-venv && rm -rf /var/lib/apt/lists/*

# Music: yt-dlp reads a track's page and streams its audio into ffmpeg. Pinned, updated on purpose;
# [default] brings what it needs to read pages that run JavaScript, with the node already here.
RUN python3 -m venv /opt/yt-dlp \
  && /opt/yt-dlp/bin/pip install --no-cache-dir "yt-dlp[default]==2026.10.21" \
  && ln -s /opt/yt-dlp/bin/yt-dlp /usr/local/bin/yt-dlp

WORKDIR /app

COPY dokja_services/dokja_discord/package.json /app/package.json
COPY dokja_services/dokja_discord/pnpm-lock.yaml /app/pnpm-lock.yaml
COPY dokja_services/dokja_discord/pnpm-workspace.yaml /app/pnpm-workspace.yaml

RUN pnpm install --frozen-lockfile

COPY dokja_services/dokja_discord /app

EXPOSE 8092

CMD ["pnpm", "start"]
