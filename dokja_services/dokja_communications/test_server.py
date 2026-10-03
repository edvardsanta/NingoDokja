import io
import json
import unittest
from urllib.error import HTTPError
from unittest.mock import MagicMock, Mock

from server import Adapter, AdapterError, project_message

# A reserved placeholder: the repository carries no address of the real API.
API = "https://discord.example/api"


class AdapterTests(unittest.TestCase):
    def test_unknown_channel_and_bad_cursor_never_reach_provider(self):
        opener = MagicMock()
        adapter = Adapter("secret", "123", opener=opener, api_base=API)
        for payload in (
            {"channel_id": "456"},
            {"channel_id": "123", "before": "../token"},
        ):
            with self.assertRaises(AdapterError):
                adapter.dispatch("history", payload)
        opener.open.assert_not_called()

    def test_history_projects_orders_and_pages(self):
        rows = [
            {
                "id": str(i),
                "content": "<script>text</script>",
                "author": {"username": "reader", "email": "private"},
                "attachments": [{"filename": "meme.png", "url": "private-url"}],
            }
            for i in range(60, 30, -1)
        ]
        opener = MagicMock()
        opener.open.return_value.__enter__.return_value = io.BytesIO(
            json.dumps(rows).encode()
        )
        answer = Adapter("secret", "123", opener=opener, api_base=API).dispatch(
            "history", {"channel_id": "123", "before": "100"}
        )
        self.assertEqual(answer["before"], "31")
        self.assertEqual(answer["messages"][0]["id"], "31")
        self.assertNotIn("private", json.dumps(answer))
        self.assertTrue(
            opener.open.call_args.args[0].full_url.startswith(
                API + "/channels/123/messages?"
            )
        )
        self.assertIn("before=100", opener.open.call_args.args[0].full_url)
        self.assertIn("limit=30", opener.open.call_args.args[0].full_url)

    def test_rate_limit_has_no_secret_or_retry(self):
        opener = MagicMock()
        opener.open.side_effect = HTTPError("https://private/", 429, "secret", {}, None)
        with self.assertRaisesRegex(AdapterError, "^rate_limited$"):
            Adapter("secret", "123", opener=opener, api_base=API).dispatch(
                "history", {"channel_id": "123"}
            )
        self.assertEqual(opener.open.call_count, 1)

    def test_without_an_api_address_nothing_is_requested(self):
        for base in ("", "ftp://discord.example", "file:///tmp", "discord.example/api"):
            opener = MagicMock()
            with self.assertRaisesRegex(AdapterError, "^not_configured$"):
                Adapter("secret", "123", opener=opener, api_base=base).dispatch(
                    "history", {"channel_id": "123"}
                )
            opener.open.assert_not_called()

    def test_a_trailing_slash_on_the_address_is_ignored(self):
        opener = MagicMock()
        opener.open.return_value.__enter__.return_value = io.BytesIO(b"[]")
        Adapter("secret", "123", opener=opener, api_base=API + "/").dispatch(
            "history", {"channel_id": "123"}
        )
        self.assertTrue(
            opener.open.call_args.args[0].full_url.startswith(
                API + "/channels/123/messages?"
            )
        )

    def test_channels_filter_guild_results(self):
        adapter = Adapter("secret", "123,456", "789")
        adapter.get = Mock(
            return_value=[
                {"id": "123", "name": "readings"},
                {"id": "000", "name": "private"},
            ]
        )
        self.assertEqual(
            adapter.dispatch("channels", {"channel_ids": ["123", "456"]}),
            {
                "channels": [
                    {"id": "123", "name": "readings"},
                    {"id": "456", "name": "456"},
                ]
            },
        )

    def test_attachment_only_and_edited_replies(self):
        row = project_message(
            {
                "id": "1",
                "author": {"bot": True},
                "edited_timestamp": "now",
                "message_reference": {"message_id": "2"},
                "attachments": [{"filename": "clip.mp4"}],
            }
        )
        self.assertTrue(row["bot"])
        self.assertTrue(row["edited"])
        self.assertEqual(row["reply_to"], "2")
        self.assertEqual(row["attachments"], ["clip.mp4"])


class HTTPAccessTests(unittest.TestCase):
    def test_credentials_are_required_before_any_adapter_call(self):
        import http.client
        import threading
        from http.server import ThreadingHTTPServer
        from server import handler_for

        adapter = Mock()
        adapter.dispatch.return_value = {"channels": []}
        token = "x" * 32
        server = ThreadingHTTPServer(("127.0.0.1", 0), handler_for(adapter, token))
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        try:
            for credential, status in (
                ("", 403),
                ("Bearer wrong", 403),
                ("Bearer " + token, 200),
            ):
                connection = http.client.HTTPConnection("127.0.0.1", server.server_port)
                connection.request(
                    "POST",
                    "/dispatch",
                    json.dumps({"action": "channels", "payload": {}}),
                    {"Authorization": credential},
                )
                response = connection.getresponse()
                self.assertEqual(response.status, status)
                self.assertNotIn(token, response.read().decode())
                connection.close()
            adapter.dispatch.assert_called_once_with("channels", {})
        finally:
            server.shutdown()
            server.server_close()
            worker.join()


if __name__ == "__main__":
    unittest.main()
