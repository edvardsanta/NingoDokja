FROM docker.io/golang:1.25-bookworm AS builder

WORKDIR /src

COPY dokja_services/dokja_vision /src/dokja_services/dokja_vision

WORKDIR /src/dokja_services/dokja_vision

RUN GOCACHE=/tmp/go-build go build -o /tmp/dokja-vision ./cmd/dokja-vision

FROM docker.io/debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /tmp/dokja-vision /app/dokja-vision

EXPOSE 8082

CMD ["/app/dokja-vision"]
