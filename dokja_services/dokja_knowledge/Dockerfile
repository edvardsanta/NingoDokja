FROM docker.io/python:3.13-slim

WORKDIR /app

RUN pip install --no-cache-dir numpy pyzmq

COPY dokja_services/dokja_knowledge /app

CMD ["python", "main.py"]
