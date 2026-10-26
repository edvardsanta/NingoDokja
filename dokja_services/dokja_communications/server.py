"""Read-only conversation adapter. Credentials and upstream metadata stay here."""

import hmac
import json
import os
import re
import signal
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import HTTPRedirectHandler, Request, build_opener

ID = re.compile(r"[0-9]{1,20}\Z")
MAX_BODY = 32 * 1024
MAX_REPLY = 1024 * 1024
# Text and announcement channels: the ones with a message history to read. Voice channels,
# categories, threads and forums are left out.
TEXT_CHANNEL_TYPES = {0, 5}


class AdapterError(Exception):
    pass


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Adapter:
    def __init__(self, token, channels, guild="", opener=None, api_base=""):
        self.token = token
        self.channels = list(
            dict.fromkeys(
                c.strip() for c in channels.split(",") if ID.fullmatch(c.strip())
            )
        )
        # DISCORD_GUILD_ID may name several servers, separated by commas.
        self.guilds = list(
            dict.fromkeys(g.strip() for g in guild.split(",") if ID.fullmatch(g.strip()))
        )
        self.opener = opener or build_opener(NoRedirect())
        # The address of the API is supplied by the operator and has no built-in value, so the
        # repository carries no address of its own. Only http(s) is accepted.
        self.api_base = (
            api_base.rstrip("/") if api_base.startswith(("http://", "https://")) else ""
        )

    def get(self, path):
        if not self.token or not self.api_base:
            raise AdapterError("not_configured")
        request = Request(
            self.api_base + path,
            headers={"Authorization": "Bot " + self.token, "User-Agent": "Ningo/1.0"},
        )
        try:
            with self.opener.open(request, timeout=4) as response:
                raw = response.read(MAX_REPLY + 1)
                if len(raw) > MAX_REPLY:
                    raise AdapterError("upstream_error")
                return json.loads(raw)
        except HTTPError as error:
            code = {
                401: "permission_denied",
                403: "permission_denied",
                404: "channel_unavailable",
                429: "rate_limited",
            }.get(error.code, "upstream_error")
            raise AdapterError(code) from None
        except (URLError, TimeoutError, ValueError, OSError):
            raise AdapterError("upstream_error") from None

    def guild_channels(self):
        """Every text channel of each configured server, with the server's name. A server
        the bot left or cannot read is reported apart instead of failing the others."""
        guilds, unavailable = [], []
        for guild in self.guilds:
            try:
                info = self.get("/guilds/" + guild)
                rows = self.get("/guilds/" + guild + "/channels")
            except AdapterError:
                unavailable.append(guild)
                continue
            if not isinstance(info, dict) or not isinstance(rows, list):
                unavailable.append(guild)
                continue
            texts = sorted(
                (
                    row
                    for row in rows
                    if isinstance(row, dict)
                    and row.get("type") in TEXT_CHANNEL_TYPES
                    and ID.fullmatch(str(row.get("id", "")))
                ),
                key=lambda row: (row.get("position") or 0, str(row.get("id"))),
            )
            guilds.append(
                {
                    "id": guild,
                    "name": str(info.get("name") or guild)[:100],
                    "channels": [
                        {"id": row["id"], "name": str(row.get("name") or row["id"])[:100]}
                        for row in texts
                    ],
                }
            )
        return {"guilds": guilds, "unavailable_guilds": unavailable}

    def channel_info(self, channel):
        """What Discord says about one channel: its server and whether it holds messages."""
        if not ID.fullmatch(str(channel)):
            raise AdapterError("invalid_request")
        row = self.get("/channels/" + channel)
        if not isinstance(row, dict):
            raise AdapterError("upstream_error")
        return {
            "id": channel,
            "guild_id": str(row.get("guild_id") or ""),
            "text": row.get("type") in TEXT_CHANNEL_TYPES,
        }

    def may_read(self, channel):
        """A configured channel, or a text channel of a configured server. With no server
        configured, only the configured channels: never every server the bot is in."""
        if channel in self.channels:
            return True
        if not self.guilds or not ID.fullmatch(str(channel)):
            return False
        try:
            info = self.channel_info(channel)
        except AdapterError:
            return False
        return info["text"] and info["guild_id"] in self.guilds

    def dispatch(self, action, payload):
        if action == "guild_channels":
            return self.guild_channels()
        if action == "channel_info":
            return self.channel_info(payload.get("channel_id", ""))
        if action == "channels":
            asked = payload.get("channel_ids", [])
            if not isinstance(asked, list) or any(
                c not in self.channels for c in asked
            ):
                raise AdapterError("permission_denied")
            names = {}
            # Each configured server is asked for names until every channel has one; a
            # channel on no configured server still shows its id.
            for guild in self.guilds:
                if not asked or all(c in names for c in asked):
                    break
                rows = self.get("/guilds/" + guild + "/channels")
                if not isinstance(rows, list):
                    raise AdapterError("upstream_error")
                names.update(
                    (row.get("id"), row.get("name", ""))
                    for row in rows
                    if isinstance(row, dict)
                )
            return {
                "channels": [
                    {"id": c, "name": str(names.get(c) or c)[:100]} for c in asked
                ]
            }
        if action != "history":
            raise AdapterError("invalid_request")
        channel = payload.get("channel_id")
        if not self.may_read(channel):
            raise AdapterError("permission_denied")
        before = payload.get("before", "")
        if not isinstance(before, str) or (before and not ID.fullmatch(before)):
            raise AdapterError("invalid_request")
        query = {"limit": 30}
        if before:
            query["before"] = before
        rows = self.get("/channels/" + channel + "/messages?" + urlencode(query))
        if not isinstance(rows, list):
            raise AdapterError("upstream_error")
        messages = [
            project_message(row)
            for row in rows[:30]
            if isinstance(row, dict) and ID.fullmatch(str(row.get("id", "")))
        ]
        messages.sort(key=lambda row: int(row["id"]))
        return {
            "channel_id": channel,
            "messages": messages,
            "before": messages[0]["id"] if len(rows) == 30 and messages else "",
        }


def short(value, limit):
    return value[:limit] if isinstance(value, str) else ""


def project_message(row):
    author = row.get("author") if isinstance(row.get("author"), dict) else {}
    attachments = (
        row.get("attachments") if isinstance(row.get("attachments"), list) else []
    )
    reference = (
        row.get("message_reference")
        if isinstance(row.get("message_reference"), dict)
        else {}
    )
    return {
        "id": short(row.get("id"), 20),
        "author": short(
            author.get("global_name") or author.get("username") or author.get("id"), 100
        ),
        "bot": author.get("bot") is True,
        "content": short(row.get("content"), 4000),
        "timestamp": short(row.get("timestamp"), 40),
        "edited": bool(row.get("edited_timestamp")),
        "reply_to": short(reference.get("message_id"), 20),
        "attachments": [
            short(item.get("filename"), 200)
            for item in attachments[:10]
            if isinstance(item, dict)
        ],
    }


def handler_for(adapter, access_token):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass  # Never log credentials, message text or upstream replies.

        def answer(self, code, value):
            raw = json.dumps(value).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def do_GET(self):
            self.answer(
                200 if self.path == "/health" else 404,
                {"status": "ok" if self.path == "/health" else "not_found"},
            )

        def do_POST(self):
            if self.path != "/dispatch":
                return self.answer(404, {"error": "not_found"})
            if len(access_token) < 32 or access_token.startswith("CHANGE_ME"):
                return self.answer(503, {"error": "not_configured"})
            supplied = self.headers.get("Authorization", "")
            if not hmac.compare_digest(
                supplied.encode(), ("Bearer " + access_token).encode()
            ):
                return self.answer(403, {"error": "permission_denied"})
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if not 0 < length <= MAX_BODY:
                    raise ValueError()
                self.connection.settimeout(5)
                data = json.loads(self.rfile.read(length))
                if not isinstance(data, dict) or not isinstance(
                    data.get("payload"), dict
                ):
                    raise ValueError()
                result = adapter.dispatch(data.get("action"), data["payload"])
                self.answer(200, {"result": result})
            except AdapterError as error:
                self.answer(502, {"error": str(error)})
            except (ValueError, TypeError, TimeoutError):
                self.answer(400, {"error": "invalid_request"})

    return Handler


def stop(_signum, _frame):
    # The container engine stops a module with SIGTERM. Leaving cleanly keeps exit
    # code 0, so a deliberate stop is not mistaken for a crash.
    raise SystemExit(0)


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, stop)
    adapter = Adapter(
        os.getenv("DISCORD_BOT_TOKEN", ""),
        os.getenv("DISCORD_SCHEDULED_MEME_CHANNEL_ID", ""),
        os.getenv("DISCORD_GUILD_ID", ""),
        api_base=os.getenv("DOKJA_DISCORD_API_BASE_URL", ""),
    )
    server = ThreadingHTTPServer(
        (os.getenv("COMMUNICATIONS_BIND", "127.0.0.1"), 8084),
        handler_for(adapter, os.getenv("DOKJA_COMMUNICATIONS_TOKEN", "")),
    )
    server.serve_forever()
