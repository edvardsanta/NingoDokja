"""Regression checks for the fixed HTTP helper; no network or page code runs."""

import importlib.util
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location(
    "web_browser", Path(__file__).with_name("web_browser.py")
)
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)


class Response:
    status_code = 200
    headers = {"content-type": "text/html"}
    closed = False
    chunks = [b"<p>Article</p><script>never executed</script>"]

    def iter_content(self, **kwargs):
        yield from self.chunks

    def close(self):
        self.closed = True


class Session:
    def __init__(self, response, options):
        self.response, self.options = response, options

    def __enter__(self):
        return self

    def __exit__(self, *args):
        pass

    def get(self, address, **kwargs):
        self.options.update(kwargs)
        for chunk in self.response.chunks:
            kwargs["content_callback"](chunk)
        return self.response


class BrowserTests(unittest.TestCase):
    def fetch(self, response, address="https://example.com/story", ip="8.8.8.8"):
        self.options = {}

        def factory(**kwargs):
            self.options.update(kwargs)
            return Session(response, self.options)

        return helper.fetch_one(
            {"url": address, "ip": ip, "timeout": 5}, factory, "resolve", "proxy"
        )

    def test_dns_pinning_no_proxy_no_redirects_and_raw_script_bytes_only(self):
        response = Response()
        result = self.fetch(response)
        self.assertEqual(result["status"], 200)
        self.assertEqual(
            self.options["curl_options"]["resolve"], ["example.com:443:8.8.8.8"]
        )
        self.assertEqual(self.options["curl_options"]["proxy"], b"")
        self.assertFalse(self.options["trust_env"])
        self.assertFalse(self.options["allow_redirects"])
        self.assertTrue(callable(self.options["content_callback"]))
        self.assertNotIn("stream", self.options)
        self.assertTrue(response.closed)

    def test_errors_and_redirects_never_become_article_text(self):
        for status in (302, 403, 429):
            response = Response()
            response.status_code = status
            result = self.fetch(response)
            self.assertEqual(result["body"], "")
            self.assertTrue(response.closed)

    def test_private_or_credentialled_targets_are_rejected(self):
        for ip in ("127.0.0.1", "10.1.2.3", "169.254.169.254", "::ffff:127.0.0.1"):
            with self.assertRaises(ValueError):
                self.fetch(Response(), ip=ip)
        for address in (
            "https://user:pass@example.com/",
            "file:///etc/passwd",
            "https://example.com:8080/",
        ):
            with self.assertRaises(ValueError):
                self.fetch(Response(), address=address)

    def test_oversized_data_is_rejected_by_callback_before_accepting_it(self):
        response = Response()
        response.chunks = [b"x" * (helper.MAX_BYTES + 1)]
        with self.assertRaises(ValueError):
            self.fetch(response)


if __name__ == "__main__":
    unittest.main()
