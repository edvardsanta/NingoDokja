FROM docker.io/python:3.13-slim

WORKDIR /app

COPY . /app

RUN pip install --no-cache-dir fastapi uvicorn transformers torch PyMuPDF mobi markdown-it-py

CMD ["python", "main.py"]
