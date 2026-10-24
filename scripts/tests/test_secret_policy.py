"""Regression checks for the loopback URL policy allowlist."""

import re
import tomllib
import unittest
from pathlib import Path


class LoopbackPolicyTests(unittest.TestCase):
    def test_sentence_punctuation_does_not_hide_host_suffixes(self):
        root = Path(__file__).resolve().parents[2]
        policy = tomllib.loads((root / ".gitleaks-policy.toml").read_text())
        rule = next(r for r in policy["rules"] if r["id"] == "dokja-external-url")
        allow = re.compile(rule["allowlists"][0]["regexes"][0])
        extract = re.compile(rule["regex"])
        scheme = "http" + "://"
        for host in ("localhost", "127.0.0.1", "0.0.0.0", "host.docker.internal"):
            for suffix in ("", ".", "/chat."):
                with self.subTest(host=host, suffix=suffix):
                    match = extract.search(f"Bridge: {scheme}{host}:8091{suffix}")
                    self.assertIsNotNone(match)
                    self.assertIsNotNone(allow.fullmatch(match[0]))
        for host in ("localhost.attacker.example", "localhost:8091.attacker.example"):
            address = scheme + host
            with self.subTest(address=address):
                self.assertIsNone(allow.fullmatch(address))


if __name__ == "__main__":
    unittest.main()
