"""Collect benchmark table cells from operator-configured public reports.

Source addresses and table selectors belong to settings.json, never to a prompt.
Only original rows, metadata and numeric cells are returned; no LLM is involved.
"""

import hashlib
import http.client
import ipaddress
import json
import math
import re
import socket
import sys
from datetime import datetime, timezone
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import urljoin, urlsplit

MAX_BYTES = 4 * 1024 * 1024
MAX_PRODUCTS = 13
MAX_SOURCES = 8
MAX_METRICS = 8


def compact(value, limit=1200):
    return " ".join(str(value).split())[:limit]


def identity(text):
    """Keep model suffixes; a nearby model is not an exact sample match."""
    text = re.sub(r"(?<=[a-z])(?=\d)|(?<=\d)(?=[a-z])", " ", text.lower())
    text = re.sub(r"[^a-z0-9]+", " ", text)
    patterns = [
        r"\b(rx|rtx|gtx)\s+(\d{3,4})(?:\s+(xtx|xt|ti|super|gre))?(?:\s+(super))?\b",
        r"\barc\s+([ab])\s*(\d{3})\b",
        r"\bryzen\s+([3579])\s+(\d{4,5})(?:\s+(x\s*3\s*d|x|g|f))?\b",
        r"\bcore\s+(ultra)\s+([3579])\s+(\d{3})(?:\s+([a-z]{1,2}))?\b",
        r"\bcore\s+i\s*([3579])\s+(\d{4,5})(?:\s+([a-z]{1,2}))?\b",
    ]
    for pattern in patterns:
        match = re.search(pattern, text)
        if match:
            return re.sub(r"\s+", "", match.group())
    return ""


class Report(HTMLParser):
    def __init__(self, body):
        super().__init__(convert_charrefs=True)
        self.title = ""
        self.metadata = {}
        self.blocks = []
        self.tables = []
        self.heading = ""
        self.table = self.row = self.cell = None
        self.capture = None
        self.buffer = []
        self.ignored = 0
        self.feed(body)

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag in ("script", "style", "noscript"):
            self.ignored += 1
        if tag == "meta":
            key = attrs.get("property", attrs.get("name", ""))
            self.metadata[key] = compact(attrs.get("content", ""), 200)
        if tag in ("title", "h1", "h2", "h3", "p", "li", "caption"):
            self.capture, self.buffer = tag, []
        if tag == "table":
            self.table = {"heading": self.heading, "caption": "", "rows": []}
        elif tag == "tr" and self.table is not None:
            self.row = []
        elif tag in ("th", "td") and self.row is not None:
            self.cell = []

    def handle_data(self, data):
        if self.ignored:
            return
        if self.cell is not None:
            self.cell.append(data)
        if self.capture:
            self.buffer.append(data)

    def handle_endtag(self, tag):
        if tag in ("script", "style", "noscript"):
            self.ignored = max(0, self.ignored - 1)
        if tag in ("th", "td") and self.cell is not None:
            self.row.append(compact(" ".join(self.cell), 1000))
            self.cell = None
        elif tag == "tr" and self.row is not None:
            self.table["rows"].append(self.row)
            self.row = None
        elif tag == "table" and self.table is not None:
            self.tables.append(self.table)
            self.table = None
        if tag == self.capture:
            text = compact(" ".join(self.buffer), 2000)
            if tag == "title":
                self.title = text
            elif tag in ("h1", "h2", "h3"):
                self.heading = text
            elif tag == "caption" and self.table is not None:
                self.table["caption"] = text
            elif text:
                self.blocks.append(text)
            self.capture = None


def checked_url(address):
    parsed = urlsplit(address)
    if (
        len(address) > 500
        or parsed.scheme not in ("http", "https")
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.port not in (None, 80 if parsed.scheme == "http" else 443)
    ):
        raise ValueError("invalid source URL")
    return parsed


def public_socket(host, port, timeout):
    answers = socket.getaddrinfo(host, port, type=socket.SOCK_STREAM)
    if not answers or any(
        not ipaddress.ip_address(a[4][0]).is_global
        or ipaddress.ip_address(a[4][0]).is_multicast
        or ipaddress.ip_address(a[4][0]).is_reserved
        for a in answers
    ):
        raise ValueError("source address is not public")
    error = None
    for _, _, _, _, target in answers:
        try:
            return socket.create_connection((target[0], port), timeout)
        except OSError as exc:
            error = exc
    raise error or ValueError("source unavailable")


class PublicHTTP(http.client.HTTPConnection):
    def connect(self):
        self.sock = public_socket(self.host, self.port, self.timeout)


class PublicHTTPS(http.client.HTTPSConnection):
    def connect(self):
        raw = public_socket(self.host, self.port, self.timeout)
        try:
            self.sock = self._context.wrap_socket(raw, server_hostname=self.host)
        except Exception:
            raw.close()
            raise


def fetch(address):
    for _ in range(4):
        parsed = checked_url(address)
        cls = PublicHTTPS if parsed.scheme == "https" else PublicHTTP
        connection = cls(parsed.hostname, parsed.port, timeout=4)
        try:
            path = parsed.path or "/"
            if parsed.query:
                path += "?" + parsed.query
            connection.request(
                "GET",
                path,
                headers={
                    "User-Agent": "NingoTechnicalReader/1.0",
                    "Accept": "text/html",
                },
            )
            response = connection.getresponse()
            if response.status in (301, 302, 303, 307, 308):
                address = urljoin(address, response.getheader("Location", ""))
                continue
            if response.status != 200:
                raise ValueError("source blocked or unavailable")
            if "html" not in response.getheader("Content-Type", "").lower():
                raise ValueError("source is not HTML")
            body = response.read(MAX_BYTES + 1)
            if len(body) > MAX_BYTES:
                raise ValueError("source exceeds size limit")
            return body.decode("utf-8", errors="replace")
        finally:
            connection.close()
    raise ValueError("source redirect limit")


def stamp(value):
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        return (
            value if len(value) == 10 else parsed.isoformat() if parsed.tzinfo else ""
        )
    except (ValueError, AttributeError):
        return ""


def measurement(cell, pattern):
    # Reject dual power modes, footnotes, blanks and ranges instead of guessing.
    match = re.fullmatch(pattern, cell.strip())
    if not match or len(match.groups()) != 1:
        return None
    value = float(match.group(1))
    return value if math.isfinite(value) and 0 < value <= 1e9 else None


def collect(products, settings, fetcher=fetch, now=None):
    if not isinstance(products, list) or len(products) > MAX_PRODUCTS:
        raise ValueError("invalid product list")
    configs = settings.get("sources", [])
    if not isinstance(configs, list) or len(configs) > MAX_SOURCES:
        raise ValueError("invalid sources")
    observed = (now or datetime.now(timezone.utc)).isoformat()
    pages, evidence, failures = {}, {}, 0
    for config in configs:
        try:
            address = config["url"]
            checked_url(address)
            if address not in pages:
                try:
                    pages[address] = Report(fetcher(address))
                except (ValueError, OSError, http.client.HTTPException):
                    pages[address] = None
            if pages[address] is None:
                raise ValueError("source unavailable")
            page = pages[address]
            # Exact captions/headers fail closed if a publisher changes table layout.
            tables = [t for t in page.tables if t["caption"] == config["caption"]]
            if len(tables) != 1:
                raise ValueError("table unavailable or ambiguous")
            table = tables[0]
            headers, *rows = table["rows"]
            if headers != config["headers"]:
                raise ValueError("table headers changed")
            context = [
                b
                for b in page.blocks
                if any(
                    s.lower() in b.lower() for s in config.get("condition_terms", [])
                )
            ]
            conditions = compact(" ".join(context), 600)
            snapshot = hashlib.sha256(
                json.dumps([table, conditions], sort_keys=True).encode()
            ).hexdigest()
            for product in products:
                model = identity(product.get("title", ""))
                if not model:
                    continue
                samples = [
                    r
                    for r in rows
                    if len(r) == len(headers)
                    and identity(r[config["model_column"]]) == model
                ]
                # Multiple board/memory variants cannot silently share a result.
                if len(samples) != 1:
                    continue
                row = samples[0]
                # Known VRAM configurations must agree, even if only one variant is listed.
                wanted_memory = re.search(
                    r"\b(\d+)\s*gb\b", product.get("title", ""), re.I
                )
                sample_memory = re.search(
                    r"\b(\d+)\s*gb\b", row[config["model_column"]], re.I
                )
                if (
                    wanted_memory
                    and sample_memory
                    and wanted_memory.group(1) != sample_memory.group(1)
                ):
                    continue
                key = product["product"]
                source = evidence.setdefault(key, {}).setdefault(
                    address,
                    {
                        "title": compact(page.title or table["caption"], 120),
                        "url": address,
                        "observed_at": observed,
                        "excerpt": "",
                        "workload": "",
                        "test_conditions": conditions,
                        "benchmarks": [],
                    },
                )
                for field, meta in [
                    ("published_at", "article:published_time"),
                    ("updated_at", "article:modified_time"),
                ]:
                    if value := stamp(page.metadata.get(meta, "")):
                        source[field] = value
                source["excerpt"] = compact(
                    source["excerpt"]
                    + " "
                    + table["caption"]
                    + ": "
                    + "; ".join(f"{h}: {c}" for h, c in zip(headers, row))
                )
                source["workload"] = compact(
                    source["workload"] + " " + table["caption"], 120
                )
                for metric in config.get("metrics", [])[:MAX_METRICS]:
                    column = metric["column"]
                    caption_mode = re.search(
                        r"\b(single|multi)[ -]?thread", table["caption"], re.I
                    )
                    header_mode = re.search(
                        r"\b(single|multi)[ -]?thread", headers[column], re.I
                    )
                    if (
                        caption_mode
                        and header_mode
                        and caption_mode.group(1).lower()
                        != header_mode.group(1).lower()
                    ):
                        continue
                    value = measurement(row[column], metric["pattern"])
                    if value is None or not conditions:
                        continue
                    source["benchmarks"].append(
                        {
                            "test": compact(
                                table["caption"] + " / " + headers[column], 160
                            ),
                            "value": value,
                            "unit": metric["unit"],
                            "higher_is_better": metric.get("higher_is_better", True),
                            "snapshot": snapshot,
                            "sample": compact(row[config["model_column"]], 120),
                        }
                    )
                source["benchmarks"] = source["benchmarks"][:MAX_METRICS]
        except (
            ValueError,
            KeyError,
            IndexError,
            TypeError,
            OSError,
            re.error,
            http.client.HTTPException,
        ):
            failures += 1
    return {
        "evidence": [
            {"product_key": k, "technical_sources": list(v.values())[:5]}
            for k, v in evidence.items()
        ],
        "failed_sources": failures,
    }


def main():
    request = json.loads(sys.stdin.buffer.read(128 * 1024))
    if (
        request.get("operation") != "price.technical.v1"
        or request.get("action") != "collect"
    ):
        raise ValueError("unsupported request")
    path = Path(__file__).with_name("settings.json")
    settings = json.loads(path.read_text())
    print(
        json.dumps(
            {
                "result": collect(
                    request.get("payload", {}).get("products", []), settings
                )
            },
            ensure_ascii=False,
        )
    )


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print(json.dumps({"error": "technical evidence collection failed"}))
        sys.exit(1)
