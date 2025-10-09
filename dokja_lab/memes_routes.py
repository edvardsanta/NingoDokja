from flask import Blueprint, render_template
from infra.sqlite.storage import SQLiteStorage
from models.Meme import Meme
from datetime import datetime

memes_bp = Blueprint("memes", __name__)

storage = SQLiteStorage("ningo_memory.db", Meme)


def safe_parse_datetime(val):
    if isinstance(val, datetime):
        return val
    if isinstance(val, str) and val:
        try:
            return datetime.fromisoformat(val)
        except Exception:
            pass
    return None


@memes_bp.route("/memes", methods=["GET"])
def show_memes():
    memes = storage.get_filtered(order_by="date_created", order_dir="DESC", limit=10)
    for meme in memes:
        meme.date_sent = safe_parse_datetime(getattr(meme, "date_sent", None))
        meme.date_created = safe_parse_datetime(getattr(meme, "date_created", None))
    return render_template("memes.html", memes=memes)
