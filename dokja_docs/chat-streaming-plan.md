# Streaming Ningo's replies

Status: planned, not implemented.

## Why

The owner tried Talk and waited on "Ningo is thinking…" with nothing on screen until the whole reply
arrived. A reply that appears as it is written is how a conversation reads; a blank wait reads as
something broken.

## Current evidence

- The desktop sends `message.created` as a **request** (ZeroMQ request/reply through the shell) and
  waits up to 120 seconds for one answer (`src/renderer/cards/assistant_conversation.tsx`).
- `ChatDomainHandler.generateReply` (`dokja_orch/internal/handlers/chat_domain_handler.go`) calls the
  generator once and gets the whole reply back.
- The runtime's `ChatAdapters.Generate` (`dokja_services/dokja_runtime/chat.go`) runs one process per
  turn and reads all of its output before it returns:
  - Claude Code: `claude -p --output-format json`;
  - Codex: `codex exec --json`, its reply written to a file;
  - Ollama: `docker compose exec dokja-ollama ollama run <model>`.
- The desktop shell has no subscription to the orchestrator's events; it only makes requests.
  Discord and WhatsApp already consume events.

## Intended behavior and boundaries

The reply appears in Talk as it is generated. The final reply, its session notice and every rule that
applies to it (citations, the reading reference, the session store) stay exactly where they are: the
streamed text is a **preview**, never the record. The conversation history plan
(`conversation-history-plan.md`) keeps that rule: a displayed message is not proof that inference
completed. Only the final reply is stored.

Preserve `Interface -> Event/Request -> Orchestrator -> Domain -> Service`. The envelope does not change.

## Proposal

1. **Runtime: stream what the adapters already produce.**
   - Claude Code: `--output-format stream-json`.
   - Codex: its `--json` event lines.
   - Ollama: the text `ollama run` prints as it goes.
   - A new runtime endpoint, `/chat/stream`, answers with newline-delimited JSON chunks
     (`{"seq": n, "text": "..."}`) and a final line carrying the whole reply. `/chat` stays as it is.
   - Chunks are bounded: a maximum per chunk, a maximum total, and the same timeout as today.
2. **Orchestrator: publish partials, answer as before.**
   - The chat handler reads the stream. For a desktop request it publishes each chunk as an event,
     `chat.reply.partial`, with:
     - `event_id`: the request's own id;
     - `payload`: `conversation_id`, `seq` and `text`.
   - It then answers the request with the final reply, exactly as now.
   - Other sources get no partials.
   - A failed stream falls back to `/chat`.
3. **Desktop: subscribe and render.**
   - The shell subscribes to `chat.reply.partial` only. It drops a partial whose `event_id` is not one
     of its own requests in flight, and hands the rest to the renderer over a new IPC channel.
   - The transcript shows the partial text in place of "thinking", as plain text. When the request
     answers, the final reply replaces the preview.
4. **Later, optional:** Discord could show "typing…" while partials flow. Editing messages as they grow
   is out of scope.

## Compatibility

- No change to the envelope, to `message.created` or to its reply.
- New, additive pieces:
  - the event type `chat.reply.partial`;
  - the runtime endpoint `/chat/stream`;
  - the desktop's subscription and its IPC channel (the allow-list grows by one event type, and no
    action is added).
- A desktop without the subscription, or a runtime without `/chat/stream`, behaves as today.

## Phases (one PR each)

1. Runtime streaming adapters and `/chat/stream`, tested with stand-in binaries that print in pieces.
2. Orchestrator: partial events for desktop requests, with the fallback to `/chat`.
3. Desktop: the shell's subscription, its filter by request id and the transcript preview.

## Risks and open questions

- **Claude Code and Codex:** their streaming formats are CLI output, not a stable API. Each adapter
  parses defensively and falls back to the whole reply.
- **Ollama via `docker compose exec`:** buffering may hold output back. Ollama's own HTTP API streams
  natively and may be the better path; this needs a check.
- **Revocation:** if the session is revoked mid-stream, the final reply wins and the preview is
  replaced.
- **Rate:** partials are coalesced (for example every 100 ms), so ZeroMQ is not flooded token by token.
