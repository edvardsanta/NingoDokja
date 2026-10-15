#!/usr/bin/env python3
"""Import the retired meme hashtag SQLite store through the public CLI contract.

The source database is read-only. Re-running the command is safe because hashtag
knowledge uses a stable source id derived from the meme URL and replaces that document.
"""

from __future__ import annotations

import argparse
import sqlite3
import subprocess
import sys
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Migrate legacy meme hashtag examples into shared Knowledge"
    )
    parser.add_argument("database", type=Path, help="legacy ningo_hashtags.db file")
    parser.add_argument(
        "--cli", default="dokja-cli", help="dokja-cli executable (default: dokja-cli)"
    )
    parser.add_argument(
        "--request-endpoint",
        default="tcp://127.0.0.1:5558",
        help="running orchestrator request endpoint",
    )
    parser.add_argument(
        "--apply", action="store_true", help="perform the import; otherwise only count rows"
    )
    return parser.parse_args()


def examples(path: Path) -> list[tuple[int, str, str, str]]:
    if not path.is_file():
        raise ValueError(f"legacy database does not exist: {path}")
    uri = f"file:{path.resolve()}?mode=ro"
    with sqlite3.connect(uri, uri=True) as connection:
        return connection.execute(
            "SELECT id, source_url, hashtag, text FROM hashtag_examples ORDER BY id"
        ).fetchall()


def main() -> int:
    args = parse_args()
    try:
        rows = examples(args.database)
    except (ValueError, sqlite3.Error) as error:
        print(f"migration source error: {error}", file=sys.stderr)
        return 2

    if not args.apply:
        print(f"dry run: {len(rows)} hashtag examples available; pass --apply to import")
        return 0

    imported = 0
    for row_id, url, hashtag, text in rows:
        command = [
            args.cli,
            "--request-endpoint",
            args.request_endpoint,
            "meme",
            "hashtag",
            "tag",
            url,
            hashtag,
            "--text",
            text,
        ]
        completed = subprocess.run(command, check=False)
        if completed.returncode != 0:
            print(f"migration stopped at legacy row {row_id}", file=sys.stderr)
            return completed.returncode
        imported += 1

    print(f"imported {imported} hashtag examples into shared Knowledge")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
