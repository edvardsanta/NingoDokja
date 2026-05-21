# Codebase Concerns

**Analysis Date:** 2025-05-14

## Tech Debt

**Moderation Logic Stubs:**
- Issue: Lacks real policy implementation, uses a permissive fallback that allows everything.
- Files: `dokja_domain/dokja_moderation/domain.go`, `dokja_orch/internal/handlers/moderation_domain_handler.go`
- Impact: Content safety checks are effectively disabled, posing a risk for production use.
- Fix approach: Implement real screening logic in a `Policy` implementation and update the orchestrator to handle non-permissive decisions.

**SQLite Storage Gaps:**
- Issue: The `get_by_id` method is a stub (`pass`). Datetime handling is noted as uncertain.
- Files: `dokja_lab/infra/sqlite/storage.py`
- Impact: Incomplete data access layer; potential for inconsistent datetime representation.
- Fix approach: Implement `get_by_id` and standardize datetime serialization/deserialization.

**Audio Infrastructure Stubs:**
- Issue: Multiple `TODO implement me` markers in the Go-based audio listening/encoding logic.
- Files: `dokja_legacy/infrastructure/audio/encoder/pcm_encoder_source.go`, `dokja_legacy/domain/audio/ffmpeg_adapter.go`
- Impact: Audio-related features in the legacy bot are likely non-functional.
- Fix approach: Implement the missing encoder and adapter methods.

## Known Bugs

**Observational Moderation:**
- Symptoms: Moderation handler receives events but doesn't actually influence the workflow.
- Files: `dokja_orch/internal/handlers/moderation_domain_handler.go`
- Trigger: Any event passing through the orchestrator.
- Workaround: Currently logged as observational only.

## Security Considerations

**Permissive Moderation Policy:**
- Risk: Default-allow behavior for all user content.
- Files: `dokja_domain/dokja_moderation/domain.go`
- Current mitigation: None (hardcoded `DecisionAllow`).
- Recommendations: Replace fallback with a strict policy or configurable filtering.

## Performance Bottlenecks

**Large Discord Voice Module:**
- Problem: `voice.ts` contains over 750 lines of code, combining complex voice state management.
- Files: `dokja_interfaces/discord/voice/voice.ts`
- Cause: All voice-related Discord logic is centralized in one file.
- Improvement path: Refactor into sub-modules for connection handling, heartbeats, and stream processing.

## Fragile Areas

**Coupling with Discordgo:**
- Files: `dokja_legacy/domain/audio/ffmpeg_adapter.go`
- Why fragile: Tightly coupled to a specific library (`discordgo`), making it hard to reuse in other interfaces (like the new TypeScript interface).
- Safe modification: Abstract the audio interface to remove direct library dependencies.

## Missing Critical Features

**Rich Moderation Feedback:**
- Problem: The system doesn't support advanced moderation outcomes like redaction or flagging.
- Blocks: Fine-grained content control.

**Service Clients Implementation:**
- Problem: Several service clients in the orchestrator likely need verification against actual service implementations.
- Files: `dokja_orch/internal/clients/`

## Test Coverage Gaps

**Incomplete Domain Testing:**
- What's not tested: Areas marked with `TODO implement me` lack functional and unit tests.
- Files: `dokja_legacy/infrastructure/audio/encoder/pcm_encoder_source.go`, `dokja_legacy/scraper/sites/memedroid_scraper.go`
- Risk: Implementation might proceed without verification, leading to regression-prone code.
- Priority: Medium

---

*Concerns audit: 2025-05-14*
