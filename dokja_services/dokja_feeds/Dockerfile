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
    python3-venv \
    libglib2.0-0 \
    libgomp1 \
    && rm -rf /var/lib/apt/lists/*


# Optional dependencies are installed at image build time, never by a plugin request.
ARG FEEDS_PLUGIN_EXTRAS=none
# content: every compatibility adapter (documents and media). media: only the media-perception adapter,
# without the document processor's torch and transformers, several gigabytes lighter.
COPY dokja_services/dokja_feeds/content-plugin-requirements.txt /tmp/content-plugin-requirements.txt
COPY dokja_services/dokja_feeds/media-plugin-requirements.txt /tmp/media-plugin-requirements.txt
RUN case "$FEEDS_PLUGIN_EXTRAS" in \
      none) ;; \
      content|media) python3 -m venv --system-site-packages /opt/feeds-plugins \
        && /opt/feeds-plugins/bin/pip install --no-cache-dir -r "/tmp/$FEEDS_PLUGIN_EXTRAS-plugin-requirements.txt" \
        && /opt/feeds-plugins/bin/pip uninstall -y opencv-python opencv-python-headless \
        && /opt/feeds-plugins/bin/pip install --no-cache-dir --no-deps opencv-python-headless==5.0.0.93 ;; \
      *) exit 2 ;; \
    esac
ENV PATH="/opt/feeds-plugins/bin:${PATH}"

COPY dokja_services/dokja_knowledge/extractors /opt/dokja_knowledge/extractors
COPY dokja_services/dokja_knowledge/plugins.py /opt/dokja_knowledge/plugins.py

COPY dokja_services/dokja_feeds/plugins.builtin /opt/dokja_feeds/plugins
COPY dokja_services/dokja_book/*.py /opt/dokja/dokja_services/dokja_book/
COPY dokja_services/dokja_meme/attachment_fetcher.py /opt/dokja/dokja_services/dokja_meme/attachment_fetcher.py
COPY dokja_lab/memes /opt/dokja/dokja_lab/memes
COPY dokja_lab/logging_config.py /opt/dokja/dokja_lab/logging_config.py

WORKDIR /app

COPY --from=builder /tmp/dokja-feeds /app/dokja-feeds

EXPOSE 5563

CMD ["/app/dokja-feeds"]
