FROM docker.io/python:3.13-slim

WORKDIR /workspace

COPY dokja_lab/requirements.txt /tmp/dokja_lab_requirements.txt
RUN pip install --no-cache-dir -r /tmp/dokja_lab_requirements.txt
# rapidocr pulls the GUI build of opencv, which needs X11 libs the slim image lacks and
# shadows the headless build nudenet uses. Keep only the headless one.
RUN pip uninstall -y opencv-python opencv-python-headless \
    && pip install --no-cache-dir --no-deps opencv-python-headless==5.0.0.93

COPY dokja_lab /workspace/dokja_lab
COPY dokja_services /workspace/dokja_services

CMD ["python", "dokja_services/dokja_meme/main.py"]
