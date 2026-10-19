# Step 07: on-demand reading experiment

Date: 2026-10-19. Tracker: [steps.json](../steps.json) step `07`.
Decisions: [ADR 0006](./decisions/0006-reading-macro-component.md) (reading host),
[ADR 0002](./decisions/0002-plugin-lifecycle.md) (plugin lifecycle),
[ADR 0001](./decisions/0001-task-workspace.md) (task workspace).

## What was built

`knowledge.read -> read (feeds.extract) -> ingest (knowledge.ingest per document)`

- **Reading host, on-demand mode.** `feeds.extract` takes `filename` and
  `content_b64`, resolves the one enabled `document.extract.v1` plugin for the
  extension, hands it a JSON request on stdin and checks its output against
  Knowledge's `Extracted` contract (`title`, `text`, `source_ref`, `key`) before
  answering. It keeps nothing. Scheduled collectors still get no stdin.
- **Knowledge's extractors as a reading-host plugin.** The
  `knowledge-extractors` example (`plugin_api: 1`, `runtime: subprocess`) imports
  Knowledge's extractor registry, and the owner's `register(registry)` plugins
  when `KNOWLEDGE_PLUGINS_DIR` is set. It serves PDF, EPUB, DOCX, HTML, text and
  feed files.
- **Orchestrated storage.** `knowledge.read` is a new additive event: a typed task
  with a `read` and an `ingest` step. The Knowledge domain sets the provenance:
  - source `file:<name>`, one id per document of a multi-document file;
  - `source_ref`;
  - tags `format:<ext>` and `reader:<plugin>`.

  Reading the same file again updates its documents. `knowledge.ingest` with a
  file is unchanged.

## Acceptance evidence

| Criterion | Evidence |
| --- | --- |
| On-demand request with input and result; scheduled manifests keep working | `TestExtractHandsTheFileToTheBoundPluginAndReturnsItsDocuments`; every existing manifest, runner, service and lifecycle test passes unchanged; `TestACollectorReadsNoInputAndAnExtractorReadsItsRequest` |
| One extractor as a plugin, Extracted contract, stored through `knowledge.ingest` with source and format provenance | `TestTheKnowledgeExtractorsExampleServesKnowledgeFormats` (real `python3` and Knowledge registry); `TestKnowledgeReadStoresWhatTheReadingHostRead`; `TestStoreReadingKeepsSourceFormatAndReaderProvenance` |
| Add or replace an extractor without router or workflow changes | `TestReplacingAnExtractorNeedsNoCodeChange`: disabling one manifest and enabling another switches the reader |
| Crash, hang, malformed output, version mismatch | `TestExtractGivesEveryPluginFailureADefinedReason`, `TestExtractNeverRunsADisabledAmbiguousOrIncompatibleProvider`, `TestExtractStopsAHungPluginAtTheCallersDeadline`, `TestKnowledgeReadGivesEachHostFailureAReasonAndStoresNothing` |
| Runtime and OS permission assumptions | Recorded below |
| Knowledge's extractors and plugins served through a reading-host adapter | `knowledge-extractors` plugin; built and run inside the production Feeds image (HTML read; `pypdf` 3.4.1 present) |

### Defined host behavior

Every `feeds.extract` failure answers an error whose message starts with a
reason. The orchestrator turns it into a step reason (`reading_<reason>`). A
failed reading stores nothing.

| Injected fault | Host reason | Plugin invoked |
| --- | --- | --- |
| No extension, empty or oversized file, bad base64 | `unsupported` / `invalid` | no |
| No enabled provider, or a disabled one | `unavailable` | no |
| `plugin_api: 2` or `document.extract.v2` manifest | `unavailable`, naming the plugin and its `incompatible:` reason | no |
| Two enabled providers for one format | `ambiguous` | no |
| Non-zero exit, output over the limit, not JSON, unknown field, wrong type, two objects | `failed` | yes |
| Hang | `timeout` at the manifest timeout or the caller's deadline, whichever comes first; the process group is killed | yes |
| Plugin answers `{"error"}`, or every text is empty | `unreadable` | yes |

The extractor's last result (`last_run`, `last_ok`, `last_error`) is listed in
`feeds.status`.

## Extension cost

- **Adding or replacing a reader for a format:** one directory with
  `plugin.json` and a program in the plugins directory, then a service restart.
  There are zero code files, and no change to the router, the workflow, the domain
  or the compose files. A replacement is two manifest edits (disable the old,
  enable the new). Two enabled providers are refused as ambiguous rather than
  chosen by order.
- **Building the on-demand mode (one-time):**

  | Module | Files | Changes |
  | --- | --- | --- |
  | Feeds | 6 Go files, 2 plugin files, 5 test files | `extract.go` (new); runner stdin; output ceiling; ROUTER server |
  | Knowledge domain | 2 code files and 1 test file | `reading.go` |
  | Orchestrator | 6 code and 3 test files | route, Feeds client, requester timeout, reading task, handler composition |
  | Images | 2 Dockerfiles | `python3`, `python3-pypdf`, Knowledge's extractors copied |
  | Compose | 3 files | `KNOWLEDGE_EXTRACTORS_DIR` |

- **Per reading:** one subprocess. Starting Python and importing the registry
  costs about 43 ms (median of 5 local runs, Python 3.14, a small text file),
  plus base64 and JSON framing of the file in both directions.
- **Server concurrency:** the Feeds server moved from a serial REP loop to
  ROUTER with one goroutine per request, about 100 lines. Without it, a reading
  that waits on its plugin would stall `feeds.status` and `feeds.items` for up to
  the 2-minute request limit. REQ clients are unchanged.
  `TestServeAnswersStatusWhileAReadingIsStillRunning` fails on the serial loop.

## Runtime and permission assumptions

- **Runtime.** The plugin is a subprocess, as ADR 0002 chose. The Feeds images
  gain Debian's `python3` and `python3-pypdf`. The production image copies
  Knowledge's `extractors/` and `plugins.py` to `/opt/dokja_knowledge`; the dev
  and lite stacks use the workspace mount. Python packages come from apt, not pip,
  so the system interpreter stays managed by Debian. The plugin runs with
  `python3 -I -B`, so the environment's `PYTHONPATH`, the user site and the
  script's own directory are not on the path, and nothing is written next to the
  read-only code.
- **Permissions.** No sandbox is claimed. A plugin runs as the Feeds container's
  user, with the container's filesystem and network. The host limits it to:
  - its own directory as working directory;
  - `PATH` plus the variables its manifest names;
  - stdin holding only its request;
  - a process group killed at the deadline;
  - bounded output (16 MiB for extract-only plugins);
  - stderr kept out of replies.

  The container is the isolation boundary, and the plugins directory is mounted
  read-only. At most 2 readings and 16 requests run at once.
- **Size.** A file is at most 20 MB, as Knowledge's upload limit. The task
  workspace (64 KiB) never holds the file or its text: the steps keep them
  outside, and the workspace records a hashed reference
  (`file:sha256:<16 hex>`), the reader and its version.

## Open findings

- The orchestrator's Knowledge client waits 5 s per request. Knowledge stores a
  document before embedding it in the same call, so a large book can time out at
  the client after it was stored. The task then reports
  `knowledge_ingest_failed`, and reading again answers `changed: false`. This
  limit predates this step and affects `knowledge.ingest` with a file too.
- No interface sends `knowledge.read` yet; the `book.*` facade (step `09`) is its
  first planned consumer.
- Bindings are still implicit: a single enabled provider is chosen and none is
  configured. Fallback lists exist in `Select` but have no configuration source.
- The Knowledge registry still decides by extension inside the adapter, so a
  format the manifest lists but the registry lacks comes back `unreadable`.
