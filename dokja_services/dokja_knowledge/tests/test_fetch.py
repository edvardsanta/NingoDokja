import socket

import pytest

import fetch
from fetch import FetchError, check_url, fetch as fetch_url, is_public, resolve_public

PUBLIC = "5.6.7.8"


def resolver_for(mapping):
    def resolve(host, port, type=None):
        answers = mapping.get(host)
        if answers is None:
            raise socket.gaierror("unknown host")
        answers = [answers] if isinstance(answers, str) else answers
        return [
            (socket.AF_INET6 if ":" in a else socket.AF_INET, socket.SOCK_STREAM, 6, "", (a, port))
            for a in answers
        ]

    return resolve


class FakeResponse:
    def __init__(self, status=200, headers=None, body=b""):
        self.status, self.headers, self._body, self._pos = status, headers or {}, body, 0

    def getheader(self, name, default=None):
        return self.headers.get(name, default)

    def read(self, size):
        chunk = self._body[self._pos : self._pos + size]
        self._pos += len(chunk)
        return chunk


class FakeConnection:
    def __init__(self, response):
        self.response, self.requests, self.closed = response, [], False

    def request(self, method, target, headers=None):
        self.requests.append((method, target, headers))

    def getresponse(self):
        return self.response

    def close(self):
        self.closed = True


def connector_for(responses):
    """Serve responses in order; remember which validated address each hop connected to."""
    served = []

    def connect(parts, address):
        served.append((parts.hostname, address))
        return FakeConnection(responses[len(served) - 1])

    connect.served = served
    return connect


HTML = {"Content-Type": "text/html; charset=utf-8"}


# ---- addresses --------------------------------------------------------------------------


@pytest.mark.parametrize(
    "address",
    ["127.0.0.1", "10.1.2.3", "192.168.0.10", "172.16.5.5", "169.254.169.254", "100.64.0.1", "0.0.0.0",
     "224.0.0.1", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1"],
)
def test_non_public_addresses_are_refused(address):
    assert is_public(address) is False


def test_a_public_address_is_allowed():
    assert is_public(PUBLIC) and is_public("2606:4700::1111")


@pytest.mark.parametrize(
    "url",
    ["file:///etc/passwd", "ftp://docs.example/a", "gopher://docs.example/", "//docs.example/x", "http:///nohost",
     "http://user:pass@docs.example/", "http://docs.example:8080/", "https://docs.example:22/", "x" * 2001],
)
def test_addresses_that_are_not_plain_http_or_https_are_refused(url):
    with pytest.raises(FetchError):
        check_url(url)


def test_default_ports_and_plain_addresses_pass():
    assert check_url("https://docs.example/a?b=1").hostname == "docs.example"
    assert check_url("http://docs.example:80/a").hostname == "docs.example"


def test_a_lookup_with_any_non_public_answer_is_refused():
    with pytest.raises(FetchError, match="non-public"):
        resolve_public("docs.example", 80, resolver_for({"docs.example": [PUBLIC, "127.0.0.1"]}))
    with pytest.raises(FetchError, match="non-public"):
        resolve_public("rebind.example", 80, resolver_for({"rebind.example": "127.0.0.1"}))
    with pytest.raises(FetchError, match="resolved"):
        resolve_public("missing.example", 80, resolver_for({}))
    assert resolve_public("docs.example", 80, resolver_for({"docs.example": PUBLIC})) == PUBLIC


# ---- fetching ---------------------------------------------------------------------------


def test_a_page_is_fetched_over_the_validated_address_only():
    connect = connector_for([FakeResponse(200, HTML, b"<p>hello</p>")])
    result = fetch_url("https://docs.example/page?x=1", resolver_for({"docs.example": PUBLIC}), connect)

    assert (result.data, result.url) == (b"<p>hello</p>", "https://docs.example/page?x=1")
    assert connect.served == [("docs.example", PUBLIC)], "the connection must use the checked address"


def test_the_request_asks_for_an_uncompressed_body_and_never_sends_credentials():
    connection = FakeConnection(FakeResponse(200, HTML, b"x"))
    fetch_url("https://docs.example/a", resolver_for({"docs.example": PUBLIC}), lambda p, a: connection)
    method, target, headers = connection.requests[0]
    assert (method, target) == ("GET", "/a")
    assert headers["Accept-Encoding"] == "identity" and "Authorization" not in headers and "Cookie" not in headers
    assert connection.closed


def test_a_redirect_to_an_internal_host_is_refused():
    resolve = resolver_for({"docs.example": PUBLIC, "inner.example": "10.0.0.5"})
    connect = connector_for([FakeResponse(302, {"Location": "http://inner.example/admin"})])
    with pytest.raises(FetchError, match="non-public"):
        fetch_url("http://docs.example/", resolve, connect)
    assert connect.served == [("docs.example", PUBLIC)], "the internal host must never be contacted"


def test_a_redirect_to_another_scheme_is_refused():
    connect = connector_for([FakeResponse(301, {"Location": "file:///etc/passwd"})])
    with pytest.raises(FetchError, match="only http and https"):
        fetch_url("http://docs.example/", resolver_for({"docs.example": PUBLIC}), connect)


def test_public_redirects_are_followed_but_only_a_few_times():
    resolve = resolver_for({"a.example": PUBLIC, "b.example": PUBLIC})
    ok = connector_for([FakeResponse(302, {"Location": "https://b.example/final"}), FakeResponse(200, HTML, b"done")])
    assert fetch_url("http://a.example/", resolve, ok).url == "https://b.example/final"

    loop = connector_for([FakeResponse(302, {"Location": "/again"})] * 10)
    with pytest.raises(FetchError, match="too many redirects"):
        fetch_url("http://a.example/", resolve, loop)


@pytest.mark.parametrize(
    "response, message",
    [
        (FakeResponse(200, {"Content-Type": "image/png"}, b"x"), "not supported"),
        (FakeResponse(200, {"Content-Type": "application/octet-stream"}, b"x"), "not supported"),
        (FakeResponse(200, {"Content-Type": "text/html", "Content-Encoding": "gzip"}, b"x"), "compressed"),
        (FakeResponse(404, HTML, b"x"), "answered 404"),
        (FakeResponse(200, {**HTML, "Content-Length": str(fetch.MAX_BYTES + 1)}, b"x"), "larger than"),
        (FakeResponse(301, {}), "without a location"),
    ],
)
def test_unsuitable_answers_are_refused(response, message):
    with pytest.raises(FetchError, match=message):
        fetch_url("http://docs.example/", resolver_for({"docs.example": PUBLIC}), connector_for([response]))


def test_a_body_that_outgrows_the_limit_while_streaming_is_cut_off(monkeypatch):
    monkeypatch.setattr(fetch, "MAX_BYTES", 100)
    body = b"a" * 500  # no Content-Length to warn us in advance
    with pytest.raises(FetchError, match="larger than"):
        fetch_url("http://docs.example/", resolver_for({"docs.example": PUBLIC}),
                  connector_for([FakeResponse(200, HTML, body)]))


def test_the_office_document_type_is_accepted_by_its_suffix():
    kind = "application/vnd.example.wordprocessingml.document"
    result = fetch_url("http://docs.example/a", resolver_for({"docs.example": PUBLIC}),
                       connector_for([FakeResponse(200, {"Content-Type": kind}, b"zip")]))
    assert result.content_type == kind


def test_a_download_that_takes_too_long_is_stopped(monkeypatch):
    clock = iter([0.0, 0.0, 999.0, 999.0, 999.0])
    monkeypatch.setattr(fetch.time, "monotonic", lambda: next(clock))
    with pytest.raises(FetchError, match="too long"):
        fetch_url("http://docs.example/", resolver_for({"docs.example": PUBLIC}),
                  connector_for([FakeResponse(200, HTML, b"abc")]))


def test_network_errors_become_fetch_errors():
    class Refusing(FakeConnection):
        def request(self, method, target, headers=None):
            raise ConnectionRefusedError("no")

    refusing = Refusing(FakeResponse())
    with pytest.raises(FetchError, match="download failed"):
        fetch_url("http://docs.example/", resolver_for({"docs.example": PUBLIC}), lambda p, a: refusing)
    assert refusing.closed
