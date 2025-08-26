# Makefile for dokja_lab development

.PHONY: help install install-dev test test-cov lint format type-check clean docker-build docker-run setup-dev

help: ## Show this help message
	@echo "dokja_lab development commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

setup-dev: ## Setup development environment
	@./setup-dev.sh

install: ## Install production dependencies
	pip install -r requirements.txt

install-dev: ## Install development dependencies
	pip install -r requirements.txt
	pip install -r requirements-dev.txt

test: ## Run tests
	pytest tests/ -v

test-cov: ## Run tests with coverage
	pytest tests/ -v --cov=. --cov-report=term --cov-report=html

lint: ## Run linting checks
	flake8 . --count --show-source --statistics

format: ## Format code
	black .
	isort .

format-check: ## Check code formatting
	black --check --diff .
	isort --check-only --diff .

type-check: ## Run type checking
	mypy . --ignore-missing-imports --exclude migrations/

quality: lint type-check format-check ## Run all quality checks

clean: ## Clean up build artifacts
	find . -type d -name __pycache__ -exec rm -rf {} +
	find . -type f -name "*.pyc" -delete
	rm -rf .pytest_cache/
	rm -rf .coverage
	rm -rf htmlcov/
	rm -rf .mypy_cache/

docker-build: ## Build Docker image
	docker build -t dokja-lab:latest .

docker-run: ## Run Docker container
	docker run --rm dokja-lab:latest

run: ## Run the application
	python main.py

migration: ## Run database migrations
	python migrations/create_tables.py

pre-commit: ## Run pre-commit hooks
	pre-commit run --all-files

# Development workflow
dev-check: format lint type-check test ## Run full development check pipeline