"""Scoped discussion context and attributed ideas; PostgreSQL only.

These tables are separate from the globally searchable document corpus.
"""

import hashlib
import json


class DiscussionStore:
    def initialize_discussions(self):
        self.conn.execute("""
            CREATE TABLE IF NOT EXISTS discussion_context (
                scope_key text PRIMARY KEY, session_id text NOT NULL,
                documents_json text NOT NULL, updated_at timestamptz NOT NULL DEFAULT now()
            );
            CREATE TABLE IF NOT EXISTS discussion_ideas (
                scope_key text NOT NULL, idea_id text NOT NULL, session_id text NOT NULL,
                event_id text NOT NULL, idea_json text NOT NULL,
                created_at timestamptz NOT NULL DEFAULT now(),
                PRIMARY KEY(scope_key, idea_id)
            );
            CREATE TABLE IF NOT EXISTS discussion_captures (
                scope_key text NOT NULL, event_id text NOT NULL,
                PRIMARY KEY(scope_key, event_id)
            );
            CREATE INDEX IF NOT EXISTS discussion_ideas_by_scope
                ON discussion_ideas(scope_key, created_at DESC);
        """)

    def discussion_load(self, scope, session):
        row = self.conn.execute(
            "SELECT documents_json FROM discussion_context WHERE scope_key=%s AND session_id=%s",
            (scope, session),
        ).fetchone()
        return {"documents": json.loads(row["documents_json"]) if row else []}

    def discussion_save(self, scope, session, documents):
        self.conn.execute(
            """INSERT INTO discussion_context(scope_key,session_id,documents_json)
               VALUES(%s,%s,%s) ON CONFLICT(scope_key) DO UPDATE SET
               session_id=excluded.session_id, documents_json=excluded.documents_json,
               updated_at=now()""",
            (scope, session, json.dumps(documents, ensure_ascii=False)),
        )
        return {"saved": True}

    def discussion_remember(self, scope, session, event, ideas):
        with self.conn.transaction():
            captured = self.conn.execute(
                """INSERT INTO discussion_captures(scope_key,event_id) VALUES(%s,%s)
                   ON CONFLICT(scope_key,event_id) DO NOTHING RETURNING event_id""",
                (scope, event),
            ).fetchone()
            if captured is None:
                return {"saved": True, "replayed": True, "submitted": 0}
            for idea in ideas:
                # Equivalent ideas replay safely even when extracted on later turns.
                identity = hashlib.sha256(
                    json.dumps(
                        [
                            idea["text"].strip().casefold(),
                            idea["quote"],
                            idea.get("url", ""),
                        ],
                        ensure_ascii=False,
                    ).encode()
                ).hexdigest()
                self.conn.execute(
                    """INSERT INTO discussion_ideas(scope_key,idea_id,session_id,event_id,idea_json)
                       VALUES(%s,%s,%s,%s,%s) ON CONFLICT(scope_key,idea_id) DO NOTHING""",
                    (
                        scope,
                        identity,
                        session,
                        event,
                        json.dumps(idea, ensure_ascii=False),
                    ),
                )
        return {"saved": True, "submitted": len(ideas)}

    def discussion_recall(self, scope, query=""):
        rows = self.conn.execute(
            """SELECT idea_json, session_id, event_id, created_at FROM discussion_ideas
               WHERE scope_key=%s
               ORDER BY ts_rank_cd(to_tsvector('simple', idea_json),
                   plainto_tsquery('simple', %s)) DESC, created_at DESC, idea_id LIMIT 8""",
            (scope, query),
        ).fetchall()
        return {
            "ideas": [
                dict(
                    json.loads(row["idea_json"]),
                    session=row["session_id"],
                    event=row["event_id"],
                    recorded_at=row["created_at"].isoformat(),
                )
                for row in rows
            ]
        }
