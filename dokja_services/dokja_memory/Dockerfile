FROM docker.io/golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y \
    libzmq3-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY dokja_services/dokja_memory /src/dokja_services/dokja_memory

WORKDIR /src/dokja_services/dokja_memory

RUN GOCACHE=/tmp/go-build go build -o /tmp/dokja-memory ./cmd/dokja-memory

FROM docker.io/debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    libzmq5 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /tmp/dokja-memory /app/dokja-memory

EXPOSE 5562

CMD ["/app/dokja-memory"]
