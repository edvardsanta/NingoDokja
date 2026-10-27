# Durable chat snapshots

The orchestrator owns `dokja_chat_sessions.db`, stored beside `DOKJA_DB_FILE`.
This is a separate SQLite file with schema version 1; existing settings,
conversation archive, Knowledge and Memory schemas are unchanged.

Each scope stores a session ID, last-seen timestamp, validated compacted summary
and recent user/assistant messages. Upserts atomically replace one snapshot;
failed writes leave the previous row intact. Restoring malformed state or a newer
schema fails explicitly. No system instructions or verified owner flags are saved.
The file is created with mode `0600`. No transcript content is logged.

This store is for one orchestrator writer. Per-scope turn serialization belongs
to its chat handler. It is not a distributed multi-orchestrator session service.

To inspect the exact saved summary locally, open the file read-only with SQLite:

```sql
SELECT scope_key, session_id, summary FROM chat_sessions;
```

The file contains private conversation data. Back it up with SQLite's online
backup mechanism, or stop its writer before copying it, including any active WAL.
Removing this file loses saved context but does not affect the other databases.
