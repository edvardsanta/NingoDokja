"""Generic socket relay for an owner-local /resolve and /media post reader.

Site extraction stays in the host worker. Credentials and settings stay local;
only public metadata and bounded, cookie-free media bytes reach the caller.
"""
from __future__ import annotations

import base64
import http.client
import json
from pathlib import Path
import re
import socket
import sys
from urllib.parse import urlsplit

MAX_MEDIA_BYTES = 16 << 20
ROOT = Path(__file__).resolve().parent
MEDIA_PATH = re.compile(r"/media/[a-f0-9]{48}\.(?:jpg|png|webp|mp4)$")


def invoke(request, config):
    if request.get("operation") != "social.resolve.v1":
        return {"handled": False}
    action = request.get("action")
    payload = request.get("payload") or {}
    connection = http.client.HTTPConnection("localhost", timeout=95)
    connection.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    connection.sock.settimeout(95)
    try:
        connection.sock.connect(str((ROOT / config["worker_socket"]).resolve()))
        if action == "read":
            connection.request("POST", "/resolve", json.dumps({"url": payload.get("url")}),
                               {"Content-Type": "application/json",
                                "Authorization": "Bearer " + config["token"]})
            response = connection.getresponse()
            data = response.read(65537)
            if response.status != 200 or len(data) > 65536:
                raise ValueError("post reader refused the request")
            return json.loads(data)
        if action != "media":
            return {"handled": False}
        target = urlsplit(payload.get("url") or "")
        origin = urlsplit(config["media_origin"])
        if (target.scheme, target.netloc) != (origin.scheme, origin.netloc) or target.query or target.fragment or target.username or not MEDIA_PATH.fullmatch(target.path):
            raise ValueError("media does not belong to this reader")
        connection.request("GET", target.path)
        response = connection.getresponse()
        content_type = response.getheader("Content-Type", "").split(";", 1)[0]
        if response.status != 200 or content_type not in ("image/jpeg", "image/png", "image/webp", "video/mp4"):
            raise ValueError("media unavailable or unsupported")
        data = response.read(MAX_MEDIA_BYTES + 1)
        if not data:
            raise ValueError("media is empty")
        if len(data) > MAX_MEDIA_BYTES:
            return {"refused": "media_too_large"}
        return {"content_type": content_type, "data_b64": base64.b64encode(data).decode("ascii")}
    finally:
        connection.close()


def main():
    try:
        request = json.load(sys.stdin)
        config = json.loads((ROOT / "settings.json").read_text())
        result = {"result": invoke(request, config)}
    except (OSError, ValueError, KeyError, TypeError, http.client.HTTPException):
        result = {"error": "local post reader unavailable"}
    json.dump(result, sys.stdout)


if __name__ == "__main__":
    main()
