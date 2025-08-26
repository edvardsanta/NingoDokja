# Dokja Lab

An event-driven Python service that handles various background tasks for the NingoDokja bot ecosystem.

## Features

- **Event Processing**: Handles events from message queues (ZeroMQ)
- **Meme Management**: Scrapes and manages memes from various sources
- **Stock Data**: Processes stock market information
- **Worker Scheduling**: Background job scheduling with APScheduler
- **Modular Design**: Plugin-based handler system

## Project Structure

```
dokja_lab/
├── ai/              # AI-related components
├── handlers/        # Event handlers
├── infra/           # Infrastructure components (storage, messaging)
├── memes/           # Meme scraping and management
├── migrations/      # Database migrations
├── models/          # Data models
├── utils/           # Utility functions
├── workers/         # Background workers
├── main.py          # Main application entry point
└── requirements.txt # Python dependencies
```

## Development

### Setup

1. Install Python 3.11+ 
2. Install dependencies:
   ```bash
   pip install -r requirements.txt
   pip install -r requirements-dev.txt
   ```

3. Install pre-commit hooks:
   ```bash
   pre-commit install
   ```

### Running

```bash
python main.py
```

### Testing

Run tests:
```bash
pytest
```

Run tests with coverage:
```bash
pytest --cov=. --cov-report=html
```

### Code Quality

Format code:
```bash
black .
isort .
```

Lint code:
```bash
flake8 .
mypy .
```

### Docker

Build and run with Docker:
```bash
docker build -t dokja-lab .
docker run dokja-lab
```

## Configuration

The application uses environment variables and configuration files. Key components:

- **Storage**: SQLite database for persistence
- **Messaging**: ZeroMQ for event communication  
- **Scheduling**: APScheduler for background tasks
- **Logging**: Centralized logging configuration

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests and linting
5. Submit a pull request

Make sure all tests pass and code follows the project's style guidelines.