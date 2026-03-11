FROM docker.io/golang:1.25.0

# Instalar dependências necessárias para Opus e Opusfile
RUN apt-get update && apt-get install -y \
    libopus-dev \
    libopusfile-dev \
    libzmq3-dev  \
    ffmpeg \
    yq \
    && rm -rf /var/lib/apt/lists/*

# Set destination for COPY
WORKDIR /app

# Download Go modules
COPY dokja_orch/go.mod dokja_orch/go.sum ./
RUN go mod download

# Copy the source code. Note the slash at the end, as explained in
# https://docs.docker.com/reference/dockerfile/#copy
COPY ./dokja_orch/cmd/ ./cmd/
COPY ./dokja_orch/internal ./internal
COPY ./generate_scraper_factory.sh ./

# Tornar o script executável
RUN chmod +x ./generate_scraper_factory.sh

# Rodar o script para gerar arquivos necessários
ARG SCRAPER_CONFIG
COPY ${SCRAPER_CONFIG} ./scraper.yaml

# Checar se o scraper foi copiado
RUN ls -l ./scraper.yaml

RUN ./generate_scraper_factory.sh

# Build
RUN GOOS=linux go build -o ningodokja ./cmd/va/main.go

# Run the application
CMD ["./ningodokja"]
