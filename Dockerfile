FROM golang:1.22.2

# Instalar dependências necessárias para Opus e Opusfile
RUN apt-get update && apt-get install -y \
    libopus-dev \
    libopusfile-dev \
    ffmpeg \
    && rm -rf /var/lib/apt/lists/*

# Set destination for COPY
WORKDIR /app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code. Note the slash at the end, as explained in
# https://docs.docker.com/reference/dockerfile/#copy
COPY . ./

# Build
RUN GOOS=linux go build -o ningodokja ./cmd/bot/main.go


# Expose the application port (uncomment if necessary)
# EXPOSE 8080

# Run the application
CMD ["./ningodokja"]
