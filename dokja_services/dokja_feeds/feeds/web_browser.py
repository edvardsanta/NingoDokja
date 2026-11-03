"""One pinned HTTP fetch. No page scripts, browser, challenge solver or redirects."""

import base64
import ipaddress
import json
import sys
from urllib.parse import urlsplit

MAX_BYTES = 2 << 20


def fetch_one(request, session_factory, resolve_option, proxy_option):
    address = request["url"]
    parsed = urlsplit(address)
    port = parsed.port or (443 if parsed.scheme == "https" else 80)
    if (
        parsed.scheme not in {"http", "https"}
        or not parsed.hostname
        or parsed.username is not None
        or port != (443 if parsed.scheme == "https" else 80)
    ):
        raise ValueError("invalid address")
    ip = ipaddress.ip_address(request["ip"])
    ip = getattr(ip, "ipv4_mapped", None) or ip
    if not ip.is_global or ip.is_multicast:
        raise ValueError("invalid destination")
    pinned = f"[{ip}]" if ip.version == 6 else str(ip)
    options = {
        resolve_option: [f"{parsed.hostname}:{port}:{pinned}"],
        proxy_option: b"",
    }
    data = bytearray()

    def receive(chunk):
        # A synchronous curl callback bounds memory before accepting each chunk;
        # a streaming response queue could buffer an unbounded fast response.
        if len(data) + len(chunk) > MAX_BYTES:
            raise ValueError("reading too large")
        data.extend(chunk)
        return len(chunk)

    # Each process has a fresh session and no inherited proxy/cookie/auth state.
    with session_factory(trust_env=False, curl_options=options) as session:
        response = session.get(
            address,
            impersonate="chrome",
            timeout=min(12, max(0.1, request["timeout"])),
            allow_redirects=False,
            content_callback=receive,
        )
        try:
            # Error/challenge and redirect bodies are never article evidence.
            body = data if response.status_code == 200 else b""
            return {
                "status": response.status_code,
                "content_type": response.headers.get("content-type", ""),
                "location": response.headers.get("location", ""),
                "challenge": response.headers.get("cf-mitigated", ""),
                "body": base64.b64encode(body).decode("ascii"),
            }
        finally:
            response.close()


def main():
    from curl_cffi import CurlOpt, requests

    request = json.loads(sys.stdin.buffer.read(8193))
    result = fetch_one(request, requests.Session, CurlOpt.RESOLVE, CurlOpt.PROXY)
    sys.stdout.write(json.dumps(result))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Never return HTML challenge tokens, URLs, credentials or tracebacks.
        sys.stdout.write('{"error":"browser_fetch_unavailable"}')
        sys.exit(1)
