import json
import unittest
from unittest.mock import patch

from server import ollama_chat_stream


class VoiceChatTests(unittest.TestCase):
    def test_streams_only_text_from_the_configured_local_model(self):
        class Response:
            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return None

            def __iter__(self):
                yield b'{"message":{"content":"Estou ouvindo."},"done":false}\n'
                yield b'{"message":{"content":""},"done":true}\n'

        messages = [{"role": "user", "content": "teste"}]
        with patch(
            "server.urllib.request.urlopen", return_value=Response()
        ) as open_url:
            frames = [
                json.loads(line)
                for line in ollama_chat_stream(messages, "qwen3.5:2b")
            ]
        self.assertEqual(frames, [{"text": "Estou ouvindo."}, {"done": True}])
        request = open_url.call_args.args[0]
        payload = json.loads(request.data)
        self.assertEqual(payload["model"], "qwen3.5:2b")
        self.assertFalse(payload["think"])
        self.assertEqual(payload["keep_alive"], "-1s")
        self.assertEqual(payload["messages"], messages)

    def test_does_not_forward_tool_calls_as_voice_text(self):
        class Response:
            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return None

            def __iter__(self):
                yield b'{"message":{"tool_calls":[{}]},"done":true}\n'

        with patch("server.urllib.request.urlopen", return_value=Response()):
            frames = [
                json.loads(line)
                for line in ollama_chat_stream(
                    [{"role": "user", "content": "hello"}], "qwen3.5:2b"
                )
            ]
        self.assertEqual(
            frames, [{"error": "voice model returned an unsupported tool call"}]
        )

if __name__ == "__main__":
    unittest.main()
