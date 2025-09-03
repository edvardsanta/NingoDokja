#!/bin/bash
# Development setup script for dokja_lab

set -e

echo "🚀 Setting up dokja_lab development environment..."

# Check if we're in the dokja_lab directory
if [[ ! -f "requirements.txt" ]]; then
    echo "❌ Please run this script from the dokja_lab directory"
    exit 1
fi

# Check Python version
echo "🐍 Checking Python version..."
python_version=$(python3 --version 2>&1 | awk '{print $2}' | cut -d. -f1,2)
required_version="3.11"

if ! python3 -c "import sys; sys.exit(0 if sys.version_info >= (3, 11) else 1)"; then
    echo "❌ Python 3.11+ is required. You have Python $python_version"
    exit 1
fi
echo "✅ Python $python_version is supported"

# Install dependencies
echo "📦 Installing dependencies..."
pip install --upgrade pip
pip install -r requirements.txt
pip install -r requirements-dev.txt

# Setup pre-commit hooks
echo "🔧 Setting up pre-commit hooks..."
pre-commit install

# Run initial code formatting
echo "🎨 Formatting code..."
black .
isort .

# Run tests to verify setup
echo "🧪 Running tests to verify setup..."
pytest tests/ -v

# Run linting
echo "🔍 Running linting checks..."
flake8 . --count --select=E9,F63,F7,F82 --show-source --statistics

echo ""
echo "🎉 Development environment setup complete!"
echo ""
echo "Available commands:"
echo "  pytest                    # Run tests"
echo "  pytest --cov=.           # Run tests with coverage"
echo "  black .                  # Format code"
echo "  isort .                  # Sort imports"
echo "  flake8 .                 # Lint code"
echo "  mypy .                   # Type checking"
echo "  pre-commit run --all-files  # Run all pre-commit hooks"
echo ""
echo "To run the application:"
echo "  python main.py"
echo ""