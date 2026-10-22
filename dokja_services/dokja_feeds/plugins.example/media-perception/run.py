"""Reuse OCR/screening/transport mechanisms without running a Meme server or scraper."""
from __future__ import annotations

import base64
import contextlib
import json
import os
import sys
from types import SimpleNamespace
from urllib.parse import urlsplit


def main():
    request = json.load(sys.stdin)
    action = request.get("action")
    if request.get("operation") != "media.perceive.v1" or action not in {"screen", "read-text", "attachment"}:
        raise ValueError("unsupported media perception operation")
    root = os.environ.get("DOKJA_REPO_ROOT", "").strip()
    if not root:
        raise ValueError("DOKJA_REPO_ROOT must name the installed media mechanisms")
    sys.path[:0] = [os.path.join(root, "dokja_services", "dokja_meme"), os.path.join(root, "dokja_lab")]
    payload = request.get("payload") or {}
    url = str(payload.get("url") or "").strip()
    parsed = urlsplit(url)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname or parsed.username or parsed.password:
        raise ValueError("a valid http(s) media url is required")
    with contextlib.redirect_stdout(sys.stderr):
        from attachment_fetcher import AttachmentFetcher, build_attachment_fetcher_from_env
        if action == "attachment":
            transport = build_attachment_fetcher_from_env()
            if transport is None:
                raise ValueError("attachment fetching is disabled")
            content, content_type = transport.fetch(url)
            result = {"url": url, "content_type": content_type, "data_b64": base64.b64encode(content).decode("ascii"), "size": len(content)}
        else:
            from memes.safety import DEFAULT_MAX_BYTES, DEFAULT_TIMEOUT_SECONDS, build_screen_from_env
            transport = AttachmentFetcher(max_bytes=DEFAULT_MAX_BYTES, timeout=DEFAULT_TIMEOUT_SECONDS)
            screen = build_screen_from_env(downloader=lambda address: transport.fetch(address)[0])
            if screen is None:
                raise ValueError("screening/OCR is disabled")
            if action == "read-text":
                text = screen.read_text(url)
                if not text.strip():
                    raise ValueError("no text found in media; provide explicit text")
                result = {"url": url, "text": text}
            else:
                inspected = screen.inspect(SimpleNamespace(url=url, title=str(payload.get("caption") or ""), tags=""))
                result = {
                    "url": url, "safe": inspected.verdict.safe,
                    "reason": inspected.verdict.reason, "text": inspected.text,
                    "detections": [{"class": detection.get("class"), "score": round(float(detection.get("score", 0)), 3)} for detection in inspected.detections],
                    "thresholds": {"exposed": screen.threshold, "strict": screen.strict_threshold if screen.strict else None},
                }
    json.dump({"result": result}, sys.stdout, ensure_ascii=False)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(type(error).__name__, file=sys.stderr)
        json.dump({"error": "media perception failed; check plugin dependencies and configuration"}, sys.stdout)
