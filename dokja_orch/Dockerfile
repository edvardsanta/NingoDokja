FROM docker.io/golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y \
    libzmq3-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY dokja_orch /src/dokja_orch
COPY dokja_domain /src/dokja_domain
COPY dokja_store /src/dokja_store

WORKDIR /src/dokja_orch

RUN GOCACHE=/tmp/go-build go build -o /tmp/va ./cmd/va
RUN GOCACHE=/tmp/go-build go build -o /tmp/source-journal-init ./cmd/source-journal-init
RUN GOCACHE=/tmp/go-build go build -o /tmp/price-history-init ./cmd/price-history-init
RUN GOCACHE=/tmp/go-build go build -o /tmp/profiles-init ./cmd/profiles-init

FROM docker.io/debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    libzmq5 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /tmp/va /app/va
COPY --from=builder /tmp/source-journal-init /app/source-journal-init
COPY --from=builder /tmp/price-history-init /app/price-history-init
COPY --from=builder /tmp/profiles-init /app/profiles-init

EXPOSE 5555 5558 8091

CMD ["/app/va"]
