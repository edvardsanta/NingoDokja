FROM python:3.12-slim

WORKDIR /app

COPY dokja_services/dokja_chat_ai /app

RUN pip install --no-cache-dir fastapi openai uvicorn

EXPOSE 8080

CMD ["python", "main.py"]
