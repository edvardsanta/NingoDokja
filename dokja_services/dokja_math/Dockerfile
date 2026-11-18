FROM python:3.13-slim

WORKDIR /app

COPY dokja_services/dokja_math /app

RUN pip install --no-cache-dir -r requirements.txt

EXPOSE 8096

CMD ["python", "main.py"]
