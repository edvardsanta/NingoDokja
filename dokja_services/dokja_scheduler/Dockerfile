FROM docker.io/golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y \
    libzmq3-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY dokja_services/dokja_scheduler /src/dokja_services/dokja_scheduler

WORKDIR /src/dokja_services/dokja_scheduler

RUN GOCACHE=/tmp/go-build go build -o /tmp/dokja-scheduler ./cmd/dokja-scheduler

FROM docker.io/debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    libzmq5 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /tmp/dokja-scheduler /app/dokja-scheduler

CMD ["/app/dokja-scheduler"]
