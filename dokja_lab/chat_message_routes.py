from flask import Blueprint, render_template
from infra.sqlite.storage import SQLiteStorage
from models.ChatMessage import ChatMessage
from datetime import datetime

chat_messages_bp = Blueprint("chat_messages", __name__)

storage = SQLiteStorage("ningo_memory.db", ChatMessage)


def safe_parse_datetime(val):
    if isinstance(val, datetime):
        return val
    if isinstance(val, str) and val:
        try:
            return datetime.fromisoformat(val)
        except Exception:
            pass
    return None


def safe_parse_list(val):
    if isinstance(val, list):
        return val
    if isinstance(val, str) and val:
        try:
            import json

            return json.loads(val)
        except Exception:
            return [val]
    return []


@chat_messages_bp.route("/chat/message", methods=["GET"])
def show_chat_messages():
    messages = storage.get_filtered(order_by="timestamp", order_dir="DESC", limit=20)
    for msg in messages:
        msg.timestamp = safe_parse_datetime(getattr(msg, "timestamp", None))
        msg.emojis = safe_parse_list(getattr(msg, "emojis", []))
    return render_template("chat_messages.html", messages=messages)
