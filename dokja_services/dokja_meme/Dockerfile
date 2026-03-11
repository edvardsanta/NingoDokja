FROM docker.io/python:3.13-slim

WORKDIR /workspace

COPY dokja_lab/requirements.txt /tmp/dokja_lab_requirements.txt
RUN pip install --no-cache-dir -r /tmp/dokja_lab_requirements.txt

COPY dokja_lab /workspace/dokja_lab
COPY dokja_services /workspace/dokja_services

CMD ["python", "dokja_services/dokja_meme/main.py"]
