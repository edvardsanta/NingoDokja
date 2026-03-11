FROM docker.io/golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y \
    libzmq3-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY dokja_orch /src/dokja_orch
COPY dokja_domain /src/dokja_domain

WORKDIR /src/dokja_orch

RUN GOCACHE=/tmp/go-build go build -o /tmp/va ./cmd/va

FROM docker.io/debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    libzmq5 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /tmp/va /app/va

EXPOSE 5555 5558 8091

CMD ["/app/va"]
