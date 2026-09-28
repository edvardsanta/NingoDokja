"""Downloads a meme attachment's raw bytes for the delivery interface to attach.

Some hosts put video downloads behind a bot-fingerprint check (Cloudflare and similar)
that a plain HTTP client fails even with correct Referer/User-Agent headers, because it
inspects the TLS/HTTP client signature itself. `curl_cffi` impersonates a real browser's
signature, which is why this uses it instead of `requests`.
"""

from __future__ import annotations

import os

from curl_cffi import requests as curl_requests

DEFAULT_MAX_BYTES = 20_000_000
DEFAULT_TIMEOUT_SECONDS = 30.0
DEFAULT_IMPERSONATE = "chrome"


class AttachmentFetchError(RuntimeError):
    """The attachment could not be downloaded (host error, timeout, or too large)."""


class AttachmentFetcher:
    def __init__(
        self,
        max_bytes: int = DEFAULT_MAX_BYTES,
        timeout: float = DEFAULT_TIMEOUT_SECONDS,
        impersonate: str = DEFAULT_IMPERSONATE,
    ) -> None:
        self.max_bytes = max_bytes
        self.timeout = timeout
        self.impersonate = impersonate

    def fetch(self, url: str) -> tuple[bytes, str | None]:
        """Returns (content, content_type). Raises AttachmentFetchError on failure."""
        try:
            with curl_requests.Session() as session:
                response = session.get(
                    url, impersonate=self.impersonate, timeout=self.timeout, stream=True
                )
                try:
                    if response.status_code != 200:
                        raise AttachmentFetchError(
                            f"host returned {response.status_code}"
                        )
                    declared = int(response.headers.get("content-length") or 0)
                    if declared > self.max_bytes:
                        raise AttachmentFetchError(f"file too large ({declared} bytes)")
                    data = bytearray()
                    for chunk in response.iter_content(chunk_size=65536):
                        data.extend(chunk)
                        if len(data) > self.max_bytes:
                            raise AttachmentFetchError("file too large")
                    return bytes(data), response.headers.get("content-type")
                finally:
                    response.close()
        except AttachmentFetchError:
            raise
        except Exception as err:
            raise AttachmentFetchError(f"download failed: {err}") from err


def build_attachment_fetcher_from_env() -> AttachmentFetcher | None:
    if os.getenv("MEME_ATTACHMENT_FETCH", "on").strip().lower() in {
        "off",
        "0",
        "false",
    }:
        return None
    return AttachmentFetcher(
        max_bytes=int(os.getenv("MEME_ATTACHMENT_MAX_BYTES", str(DEFAULT_MAX_BYTES))),
        timeout=float(
            os.getenv("MEME_ATTACHMENT_TIMEOUT", str(DEFAULT_TIMEOUT_SECONDS))
        ),
    )
