import importlib.util
import socket
import unittest
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "collector", Path(__file__).with_name("run.py")
)
collector = importlib.util.module_from_spec(spec)
spec.loader.exec_module(collector)

BODY = """<html><head><title>Example lab tests</title>
<meta property="article:published_time" content="2026-01-03T12:00:00Z">
<meta property="article:modified_time" content="2026-02-04T12:00:00Z"></head><body>
<p>Test system: Example CPU, 32GB RAM, driver version 1.2.</p>
<table><caption>Raster test</caption><tr><th>GPU</th><th>1080p</th></tr>
<tr><td>Radeon RX 7600</td><td>70.0</td></tr>
<tr><td>Radeon RX 7600 XT</td><td>82.0</td></tr>
<tr><td>GeForce RTX 4060</td><td>72.0</td></tr></table>
<table><caption>Ray test</caption><tr><th>GPU</th><th>1080p</th></tr>
<tr><td>Radeon RX 7600</td><td>20.0</td></tr></table></body></html>"""


def settings(caption="Raster test"):
    return {
        "sources": [
            {
                "url": "https://bench.example/report",
                "caption": caption,
                "headers": ["GPU", "1080p"],
                "model_column": 0,
                "condition_terms": ["Test system"],
                "metrics": [
                    {"column": 1, "unit": "fps", "pattern": r"([0-9]+(?:\.[0-9]+)?)"}
                ],
            }
        ]
    }


class CollectorTests(unittest.TestCase):
    def test_collects_cells_dates_conditions_and_exact_model_suffixes(self):
        products = [
            {"product": "ref", "title": "Placa RX7600 Challenger 8GB"},
            {"product": "rival", "title": "GPU RTX 4060 OC"},
        ]
        result = collector.collect(products, settings(), lambda _: BODY)
        self.assertEqual(result["failed_sources"], 0)
        source = result["evidence"][0]["technical_sources"][0]
        self.assertEqual(source["benchmarks"][0]["value"], 70)
        self.assertEqual(source["published_at"], "2026-01-03T12:00:00+00:00")
        self.assertIn("32GB RAM", source["test_conditions"])
        self.assertIn("GPU: Radeon RX 7600", source["excerpt"])
        self.assertNotIn("82.0", source["excerpt"])
        other = result["evidence"][1]["technical_sources"][0]["benchmarks"][0]
        self.assertEqual(other["snapshot"], source["benchmarks"][0]["snapshot"])

    def test_one_fetch_retains_distinct_raster_and_ray_tests(self):
        config = settings()
        config["sources"] += settings("Ray test")["sources"]
        fetched = []

        def fetch(url):
            fetched.append(url)
            return BODY

        result = collector.collect(
            [{"product": "ref", "title": "RX 7600"}], config, fetch
        )
        self.assertEqual(len(fetched), 1)
        metrics = result["evidence"][0]["technical_sources"][0]["benchmarks"]
        self.assertEqual(len(metrics), 2)
        self.assertNotEqual(metrics[0]["snapshot"], metrics[1]["snapshot"])

    def test_layout_changes_and_duplicate_memory_variants_fail_closed(self):
        changed = BODY.replace("<th>1080p</th>", "<th>1440p</th>")
        result = collector.collect(
            [{"product": "ref", "title": "RX 7600"}], settings(), lambda _: changed
        )
        self.assertEqual(result["evidence"], [])
        ambiguous = BODY.replace("Radeon RX 7600 XT", "Radeon RX 7600 16GB")
        result = collector.collect(
            [{"product": "ref", "title": "RX 7600 8GB"}],
            settings(),
            lambda _: ambiguous,
        )
        self.assertEqual(result["evidence"], [])

    def test_ambiguous_power_modes_and_missing_conditions_are_not_measurements(self):
        for body in [
            BODY.replace("70.0", "70.0 / 75.0"),
            BODY.replace("Test system:", "Unidentified setup:"),
        ]:
            result = collector.collect(
                [{"product": "ref", "title": "RX 7600"}], settings(), lambda _: body
            )
            source = result["evidence"][0]["technical_sources"][0]
            self.assertEqual(source["benchmarks"], [])
            self.assertTrue(source["excerpt"])

    def test_fetch_failures_do_not_invent_sources_or_expose_errors(self):
        def fail(_):
            raise OSError("private transport detail")

        result = collector.collect(
            [{"product": "ref", "title": "RX 7600"}], settings(), fail
        )
        self.assertEqual(result, {"evidence": [], "failed_sources": 1})

    def test_private_and_mixed_dns_are_rejected_before_connecting(self):
        answers = [
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", (ip, 443))
            for ip in ["1.1.1.1", "127.0.0.1"]
        ]
        with patch.object(
            collector.socket, "getaddrinfo", return_value=answers
        ), patch.object(collector.socket, "create_connection") as connect:
            with self.assertRaises(ValueError):
                collector.public_socket("bench.example", 443, 4)
            connect.assert_not_called()
        for address in [
            "https://user:secret@bench.example",
            "file:///private",
            "https://bench.example:8443",
        ]:
            with self.assertRaises(ValueError):
                collector.checked_url(address)

    def test_cpu_model_identity_keeps_architecture_and_suffix(self):
        for title in [
            "Ryzen 7 9700X",
            "Intel Core i5-14600KF",
            "Core Ultra 7 265K",
            "Arc B580",
        ]:
            self.assertTrue(collector.identity(title), title)
        self.assertNotEqual(
            collector.identity("Ryzen 7 9700X"), collector.identity("Ryzen 7 9700X3D")
        )


class ConsistencyTests(unittest.TestCase):
    def test_conflicting_suite_headers_and_vram_are_not_substituted(self):
        config = settings("Multi-Threaded test")
        config["sources"][0]["headers"] = ["GPU", "Single-Threaded score"]
        body = BODY.replace("Raster test", "Multi-Threaded test").replace(
            "<th>1080p</th>", "<th>Single-Threaded score</th>"
        )
        result = collector.collect(
            [{"product": "ref", "title": "RX 7600"}], config, lambda _: body
        )
        self.assertEqual(
            result["evidence"][0]["technical_sources"][0]["benchmarks"], []
        )
        body = BODY.replace("Radeon RX 7600</td>", "Radeon RX 7600 16GB</td>")
        result = collector.collect(
            [{"product": "ref", "title": "RX 7600 8GB"}], settings(), lambda _: body
        )
        self.assertEqual(result["evidence"], [])


if __name__ == "__main__":
    unittest.main()
