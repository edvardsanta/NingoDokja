# Streaming Ningo's replies

Status: implemented in #118 (runtime), #119 (orchestrator) and #121 (desktop), in review.

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
   - A new runtime endpoint, `/chat/stream`, answers with newline-delimited JSON:
     `{"preview": "..."}` lines carrying **the text written so far** (each replaces the one before,
     so a lost line costs nothing), then the reply `/chat` would give, or `{"error": ...}`.
     `/chat` stays as it is.
   - Previews are coalesced to at most one every 100 ms and bounded by the reply's own size limit
     and timeout.
2. **Orchestrator: a streamed response on the HTTP bridge.**
   - The orchestrator has no outbound pub/sub channel: its publisher is never wired, and its request
     socket answers one request at a time. So the owner chose a streamed response on the existing
     authenticated HTTP bridge, where previews reach only the caller.
   - `POST /orchestrator/chat/stream` takes the envelope the request socket takes, a desktop
     `message.created` only, with the bridge's bearer credential. It answers newline-delimited JSON:
     `{"preview": ...}` lines, then the response the request socket would give.
   - The preview travels in the request's context. The chat handler streams only when asked and when
     its generator can; every other path is unchanged.
   - An older runtime without `/chat/stream` is asked through `/chat`.
3. **Desktop: send chat turns to the stream and render.**
   - The shell sends a Talk turn to the bridge stream when it has the bridge address and credential,
     and over the request socket otherwise, as today. It hands the previews to the renderer over an IPC
     channel.
   - The transcript shows the partial text in place of "thinking", as plain text. When the response
     arrives, the final reply replaces the preview.
4. **Later, optional:** Discord could show "typing…" while partials flow. Editing messages as they grow
   is out of scope.

## Compatibility

- No change to the envelope, to `message.created` or to its reply.
- New, additive pieces:
  - the runtime endpoint `/chat/stream`;
  - the bridge route `/orchestrator/chat/stream`;
  - the desktop's IPC channel for previews.
- The desktop needs the bridge credential (`DOKJA_GATEWAY_INGRESS_TOKEN`) and address to stream.
- A desktop without them, or a runtime without `/chat/stream`, behaves as today.

## Phases (one PR each)

1. Runtime streaming adapters and `/chat/stream`, tested with stand-in binaries that print in pieces.
2. Orchestrator: the bridge stream route, previews through the request context, and the fallback to
   `/chat`.
3. Desktop: the shell sends Talk turns to the stream when configured, and the transcript previews.

## Risks and open questions

- **Claude Code and Codex:** their streaming formats are CLI output, not a stable API. Each adapter
  parses defensively and falls back to the whole reply.
- **Ollama via `docker compose exec`:** buffering may hold output back. Ollama's own HTTP API streams
  natively and may be the better path; this needs a check.
- **Revocation:** if the session is revoked mid-stream, the final reply wins and the preview is
  replaced.
- **Rate:** partials are coalesced (for example every 100 ms), so ZeroMQ is not flooded token by token.
