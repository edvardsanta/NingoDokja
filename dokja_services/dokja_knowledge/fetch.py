"""Fetch a page a user names, without letting the service be used to reach internal networks.

The orchestrator port is unauthenticated, so a URL fetcher is a request proxy unless it is
careful. Every hop is checked the same way:

* only http and https, on the default ports, with no credentials in the URL;
* the host name is resolved once and *every* address it resolves to must be public
  (loopback, private, link-local, shared, multicast and reserved ranges are refused);
* the connection is then made to that validated address, so a second DNS answer cannot
  redirect it (DNS rebinding);
* redirects are followed by hand, at most a few, and each one is checked again;
* the answer is limited in size and time, is never decompressed, and must be a known type.

Nothing here names a website; the address always comes from the caller.
"""

from __future__ import annotations

import http.client
import ipaddress
import socket
import ssl
import time
from dataclasses import dataclass
from urllib.parse import urljoin, urlsplit

from extractors import CONTENT_TYPES

MAX_BYTES = 20_000_000
MAX_REDIRECTS = 3
MAX_URL_LENGTH = 2_000
CONNECT_TIMEOUT = 10.0
TOTAL_TIMEOUT = 30.0
ALLOWED_PORTS = {"http": 80, "https": 443}
USER_AGENT = "dokja-knowledge/1.0"
# Types the extractors understand, plus the office document type, matched by its suffix.
EXTRA_TYPE_SUFFIXES = ("wordprocessingml.document",)


class FetchError(ValueError):
    """The address was refused or the page could not be fetched."""


@dataclass(frozen=True)
class Fetched:
    data: bytes
    content_type: str
    url: str


def is_public(address: str) -> bool:
    ip = ipaddress.ip_address(address)
    if isinstance(ip, ipaddress.IPv6Address) and ip.ipv4_mapped is not None:
        ip = ip.ipv4_mapped
    return ip.is_global and not ip.is_multicast


def check_url(url: str):
    if len(url) > MAX_URL_LENGTH:
        raise FetchError("the address is too long")
    parts = urlsplit(url)
    if parts.scheme not in ALLOWED_PORTS:
        raise FetchError("only http and https addresses are allowed")
    if not parts.hostname:
        raise FetchError("the address has no host")
    if parts.username or parts.password:
        raise FetchError("addresses with credentials are not allowed")
    try:
        port = parts.port or ALLOWED_PORTS[parts.scheme]
    except ValueError as err:
        raise FetchError("the address has an invalid port") from err
    if port != ALLOWED_PORTS[parts.scheme]:
        raise FetchError("only the default http and https ports are allowed")
    return parts


def resolve_public(host: str, port: int, resolver=socket.getaddrinfo) -> str:
    """Resolve host and return one address, refusing the lookup if any answer is not public."""
    try:
        answers = resolver(host, port, type=socket.SOCK_STREAM)
    except OSError as err:
        raise FetchError("the host could not be resolved") from err
    addresses = []
    for family, _, _, _, sockaddr in answers:
        if family in (socket.AF_INET, socket.AF_INET6):
            addresses.append(sockaddr[0].split("%")[0])
    if not addresses:
        raise FetchError("the host has no usable address")
    for address in addresses:
        if not is_public(address):
            raise FetchError("the address points to a non-public network and is refused")
    return addresses[0]


class _Pinned:
    """Mixin: connect to a validated address instead of resolving the name again."""

    pinned_address = ""

    def _open_socket(self):
        return socket.create_connection((self.pinned_address, self.port), timeout=self.timeout)


class _PinnedHTTP(_Pinned, http.client.HTTPConnection):
    def connect(self):
        self.sock = self._open_socket()


class _PinnedHTTPS(_Pinned, http.client.HTTPSConnection):
    def connect(self):
        raw = self._open_socket()
        self.sock = self._context.wrap_socket(raw, server_hostname=self.host)


def _connect(parts, address: str):
    factory = _PinnedHTTPS if parts.scheme == "https" else _PinnedHTTP
    kwargs = {"context": ssl.create_default_context()} if parts.scheme == "https" else {}
    connection = factory(parts.hostname, ALLOWED_PORTS[parts.scheme], timeout=CONNECT_TIMEOUT, **kwargs)
    connection.pinned_address = address
    return connection


def _type_allowed(content_type: str) -> bool:
    base = content_type.split(";")[0].strip().lower()
    return base in CONTENT_TYPES or base.endswith(EXTRA_TYPE_SUFFIXES)


def fetch(url: str, resolver=socket.getaddrinfo, connector=_connect) -> Fetched:
    """Fetch one address. `resolver` and `connector` exist so tests never touch the network."""
    deadline = time.monotonic() + TOTAL_TIMEOUT
    current = url.strip()
    for _ in range(MAX_REDIRECTS + 1):
        parts = check_url(current)
        address = resolve_public(parts.hostname, ALLOWED_PORTS[parts.scheme], resolver)
        connection = connector(parts, address)
        try:
            target = parts.path or "/"
            if parts.query:
                target += "?" + parts.query
            connection.request(
                "GET", target,
                headers={"Host": parts.hostname, "User-Agent": USER_AGENT, "Accept-Encoding": "identity",
                         "Accept": "text/html, application/pdf, application/xml, text/plain, */*;q=0.1"},
            )
            response = connection.getresponse()
            if response.status in (301, 302, 303, 307, 308):
                location = response.getheader("Location")
                if not location:
                    raise FetchError("the server redirected without a location")
                current = urljoin(current, location)
                continue
            if response.status != 200:
                raise FetchError(f"the server answered {response.status}")
            content_type = response.getheader("Content-Type", "")
            if not _type_allowed(content_type):
                raise FetchError(f"content type {content_type.split(';')[0].strip() or '(none)'} is not supported")
            if response.getheader("Content-Encoding", "identity").lower() != "identity":
                raise FetchError("compressed responses are not accepted")
            declared = response.getheader("Content-Length")
            if declared and declared.isdigit() and int(declared) > MAX_BYTES:
                raise FetchError("the page is larger than the allowed size")
            chunks, total = [], 0
            while True:
                if time.monotonic() > deadline:
                    raise FetchError("the download took too long")
                chunk = response.read(64 * 1024)
                if not chunk:
                    break
                total += len(chunk)
                if total > MAX_BYTES:
                    raise FetchError("the page is larger than the allowed size")
                chunks.append(chunk)
            return Fetched(b"".join(chunks), content_type, current)
        except FetchError:
            raise
        except (OSError, http.client.HTTPException) as err:
            raise FetchError(f"the download failed: {type(err).__name__}") from err
        finally:
            connection.close()
    raise FetchError("too many redirects")
