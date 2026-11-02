import base64
import io
import tempfile
import unittest
from pathlib import Path
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
        for mime, data in (("text/html", b"error"), ("video/mp4", b"")):
            response = MagicMock(status=200)
            response.getheader.return_value = mime
            response.read.return_value = data
            connection = MagicMock()
            connection.getresponse.return_value = response
            with patch("run.http.client.HTTPConnection", return_value=connection), patch("run.socket.socket"):
                with self.assertRaises(ValueError):
                    run.invoke({"operation": "social.resolve.v1", "action": "media", "payload": {"url": config["media_origin"] + "/media/" + "a" * 48 + ".mp4"}}, config)

    def test_media_over_the_limit_is_refused_as_too_large(self):
        config = {"worker_socket": "worker.sock", "media_origin": "http://localhost:8791", "token": "test"}
        response = MagicMock(status=200)
        response.getheader.return_value = "video/mp4"
        response.read.return_value = b"x" * (run.MAX_MEDIA_BYTES + 1)
        connection = MagicMock()
        connection.getresponse.return_value = response
        with patch("run.http.client.HTTPConnection", return_value=connection), patch("run.socket.socket"):
            result = run.invoke({"operation": "social.resolve.v1", "action": "media", "payload": {"url": config["media_origin"] + "/media/" + "a" * 48 + ".mp4"}}, config)
        self.assertEqual(result, {"refused": "media_too_large"})

    def media_file(self, stream, destination, mime="video/mp4"):
        response = MagicMock(status=200)
        response.getheader.return_value = mime
        response.read.side_effect = stream.read
        connection = MagicMock()
        connection.getresponse.return_value = response
        config = {"worker_socket": "worker.sock", "media_origin": "http://localhost:8791", "token": "test"}
        payload = {"url": config["media_origin"] + "/media/" + "a" * 48 + ".mp4", "destination": destination}
        with patch("run.http.client.HTTPConnection", return_value=connection), patch("run.socket.socket"):
            return run.invoke({"operation": "social.resolve.v1", "action": "media_file", "payload": payload}, config), response

    def test_media_file_writes_past_the_inline_limit_in_small_reads(self):
        contents = b"x" * (run.MAX_MEDIA_BYTES + 1)
        with tempfile.TemporaryDirectory() as folder:
            destination = Path(folder) / "clip"
            result, response = self.media_file(io.BytesIO(contents), str(destination))
            self.assertEqual(result, {"content_type": "video/mp4"})
            self.assertEqual(destination.read_bytes(), contents)
        self.assertLessEqual(max(call.args[0] for call in response.read.call_args_list), 1 << 16)

    def test_media_file_over_the_limit_is_refused_as_too_large(self):
        with tempfile.TemporaryDirectory() as folder, patch("run.MAX_FILE_BYTES", 4):
            result, _ = self.media_file(io.BytesIO(b"12345"), str(Path(folder) / "clip"))
        self.assertEqual(result, {"refused": "media_too_large"})

    def test_media_file_needs_a_new_absolute_destination(self):
        with tempfile.TemporaryDirectory() as folder:
            taken = Path(folder) / "taken"
            taken.write_bytes(b"keep")
            for destination in (None, "relative", str(taken)):
                with self.subTest(destination=destination), self.assertRaises((ValueError, OSError)):
                    self.media_file(io.BytesIO(b"clip"), destination)
            self.assertEqual(taken.read_bytes(), b"keep")


if __name__ == "__main__":
    unittest.main()
