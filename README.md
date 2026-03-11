# Ningo Dokja

## Overview

Ningo Dokja is a versatile bot designed to enhance my server's engagement through reading messages, 
sending news and memes, and summarizing books.

## Architecture

This project consists of two main components:
- **Main Bot (Go)**: Discord bot handling real-time interactions
- **Dokja Lab (Python)**: Event-driven service for background processing, data scraping, and AI tasks

## Features

1. **Message Reader**
   - Reads messages from specified channels.
   - Can respond or react to certain keywords or phrases.
  
2. **News Sender**
   - Sends the latest news to a designated channel.
   -  TODO: [] Configurable to include news from various categories (e.g., technology, sports, entertainment).

3. **Meme Sender**
   - TODO: [] Automatically posts memes to a designated channel at specified intervals.
   - TODO: [] Sources memes from popular meme websites or user-submitted content.

4. **Book Summarizer**
   - TODO: [] Reads and summarizes books.
   - TODO: [] Provides concise summaries or detailed chapter-by-chapter breakdowns.
   - TODO: [] Users can request summaries of specific books.

## Development

### Go Component
The Go orchestrator code now lives under `dokja_orch/`.

Quick setup:
```bash
cd dokja_orch
go test ./...
go run ./cmd/va
```

### Python Component (dokja_lab)
For Python development, see the dedicated [dokja_lab README](dokja_lab/README.md).

Quick setup:
```bash
cd dokja_lab
./setup-dev.sh
make help
```

## Commands

### General Commands
- `/help` - Displays the list of available commands.

### News Commands
- `!news` - Sends the latest news from CNN.



## Contributing

Feel free to fork this project and modify it to suit your needs. Your contributions are welcome.
