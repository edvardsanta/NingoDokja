FROM docker.io/golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y \
    libzmq3-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY dokja_services/dokja_feeds /src/dokja_services/dokja_feeds

WORKDIR /src/dokja_services/dokja_feeds

RUN GOCACHE=/tmp/go-build go build -o /tmp/dokja-feeds ./cmd/dokja-feeds

FROM docker.io/debian:bookworm-slim

# python3, pypdf and Knowledge's extractors run the knowledge-extractors reading plugin
# (plugins.example); KNOWLEDGE_EXTRACTORS_DIR points the plugin at /opt/dokja_knowledge.
RUN apt-get update && apt-get install -y \
    ca-certificates \
    libzmq5 \
    python3 \
    python3-pypdf \
    && rm -rf /var/lib/apt/lists/*

COPY dokja_services/dokja_knowledge/extractors /opt/dokja_knowledge/extractors
COPY dokja_services/dokja_knowledge/plugins.py /opt/dokja_knowledge/plugins.py

WORKDIR /app

COPY --from=builder /tmp/dokja-feeds /app/dokja-feeds

EXPOSE 5563

CMD ["/app/dokja-feeds"]
