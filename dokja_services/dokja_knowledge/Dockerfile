FROM docker.io/python:3.13-slim

WORKDIR /app

COPY dokja_services/dokja_knowledge/postgres-requirements.txt /tmp/postgres-requirements.txt

RUN pip install --no-cache-dir numpy pyzmq pypdf -r /tmp/postgres-requirements.txt

COPY dokja_services/dokja_knowledge /app

CMD ["python", "main.py"]
