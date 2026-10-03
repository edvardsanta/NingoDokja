FROM python:3.12-slim
WORKDIR /app
COPY dokja_services/dokja_communications/server.py /app/server.py
ENV COMMUNICATIONS_BIND=0.0.0.0
USER 65534:65534
EXPOSE 8084
CMD ["python", "server.py"]
