import base64
import unittest
from unittest.mock import MagicMock, patch

import run


class RelayTests(unittest.TestCase):
    def test_media_uses_the_worker_socket_and_returns_bounded_bytes(self):
        response = MagicMock(status=200)
        response.getheader.return_value = "video/mp4"
        response.read.return_value = b"video"
        connection = MagicMock()
        connection.getresponse.return_value = response
        config = {"worker_socket": "worker.sock", "media_origin": "http://localhost:8791", "token": "test"}
        with patch("run.http.client.HTTPConnection", return_value=connection), patch("run.socket.socket"):
            result = run.invoke({"operation": "social.resolve.v1", "action": "media", "payload": {"url": config["media_origin"] + "/media/" + "a" * 48 + ".mp4"}}, config)
        self.assertEqual(result["data_b64"], base64.b64encode(b"video").decode())
        self.assertEqual(result["content_type"], "video/mp4")
        self.assertEqual(response.read.call_args.args[0], run.MAX_MEDIA_BYTES + 1)
        connection.close.assert_called_once()

    def test_media_cannot_select_other_origins_or_paths(self):
        config = {"worker_socket": "worker.sock", "media_origin": "http://localhost:8791", "token": "test"}
        for url in ("http://other.example/media/x.mp4", "file:///etc/passwd", "http://localhost:8791/resolve", "http://localhost:8791/media/../secret"):
            with self.subTest(url=url), patch("run.http.client.HTTPConnection"), patch("run.socket.socket"):
                with self.assertRaises(ValueError):
                    run.invoke({"operation": "social.resolve.v1", "action": "media", "payload": {"url": url}}, config)

    def test_media_rejects_oversized_or_wrong_type_results(self):
        config = {"worker_socket": "worker.sock", "media_origin": "http://localhost:8791", "token": "test"}
        for mime, data in (("text/html", b"error"), ("video/mp4", b""), ("video/mp4", b"x" * (run.MAX_MEDIA_BYTES + 1))):
            response = MagicMock(status=200)
            response.getheader.return_value = mime
            response.read.return_value = data
            connection = MagicMock()
            connection.getresponse.return_value = response
            with patch("run.http.client.HTTPConnection", return_value=connection), patch("run.socket.socket"):
                with self.assertRaises(ValueError):
                    run.invoke({"operation": "social.resolve.v1", "action": "media", "payload": {"url": config["media_origin"] + "/media/" + "a" * 48 + ".mp4"}}, config)


if __name__ == "__main__":
    unittest.main()
