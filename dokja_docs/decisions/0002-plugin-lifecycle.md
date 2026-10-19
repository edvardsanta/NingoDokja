# ADR 0002: common lifecycle and capability-specific plugin contracts

Date: 2026-10-18. Status: accepted for prototype. Implementation: step `05`.
The Feeds host reads `plugin_api: 1` manifests next to the unchanged legacy shape and
resolves bindings deterministically ([manifest.go](../../dokja_services/dokja_feeds/feeds/manifest.go),
[binding.go](../../dokja_services/dokja_feeds/feeds/binding.go)); the Knowledge registry
records provenance and replacements without changing `register(registry)`. Under
[ADR 0006](./0006-reading-macro-component.md) versioned extractors are reading-host
subprocess plugins, so no versioned in-process Knowledge registry is built.
Related: [decision index](./README.md), [workspace](./0001-task-workspace.md).

## Context

[Feeds](../../dokja_services/dokja_feeds/feeds/manifest.go) validates strict JSON
manifests and executes external commands. Its current decoder rejects unknown
fields. [Knowledge](../../dokja_services/dokja_knowledge/plugins.py) imports
Python files in sorted filename order and calls `register(registry)`.
Its [registry](../../dokja_services/dokja_knowledge/extractors/__init__.py)
currently replaces an earlier extractor/fetcher registration with a later one.
One lifecycle vocabulary can support both hosts without forcing identical
execution mechanisms or generic untyped operation payloads.

## Decision and ownership

Keep each plugin under a macro capability's host. The first supported extension
types are infrastructure/provider plugins: source collection, document fetching,
and extraction. Each operation retains its own input/output schema.

Domain-policy extensions, if added later, are domain-owned. Workflow recipes are
orchestrator-owned declarations of registered domain actions. Provider plugins
cannot change routing, eligibility, moderation, or workflow authorization.
The prototype does not introduce executable policy or workflow plugins.

Assume owner-controlled plugin directories, matching existing deployment.
Use subprocess JSON for the new extractor experiment; retain trusted in-process
Knowledge plugins through an adapter. Subprocesses provide a separate failure
domain, but permissions and resource isolation must be specified by the host.
This decision adds neither a sandbox claim nor a WebAssembly dependency.

## Lifecycle contract

The internal plugin descriptor contains:

- Stable plugin ID and owning host ID.
- Lifecycle schema version (`plugin_api`) and plugin package version (`version`).
- Provided capability identifiers including their operation contract version,
  such as `document.extract.v1`.
- Runtime kind, declared configuration-variable names, activation state, and
  invocation limits.
- Compatibility result, lifecycle state, last invocation result, and timestamps.

These three versions serve different purposes: lifecycle schema version,
operation contract version, and plugin implementation/package version. The
first implementation supports lifecycle version `1` exactly. It refuses an
explicit unsupported version with a stable compatibility reason. Package-version
ordering must not be used as an implicit capability selection policy.

Discovery reads metadata and reports invalid, disabled, or incompatible plugins
without executing them. Activation is a separate configured operation. New
versioned plugins are disabled unless explicitly enabled. Invocation reports
success, timeout, cancellation, invalid output, or execution failure separately
from compatibility/activation state. Preserve a last successful cache only when
the capability contract permits it, with its observation time and staleness.

## Legacy compatibility

For Feeds, an omitted `plugin_api` identifies the existing manifest shape and
selects the legacy adapter. Continue to validate that shape strictly, including
unknown-field rejection and existing enablement, interval, timeout, environment,
and output semantics. An explicit `plugin_api: 1` selects the new validator.
Do not reinterpret an explicitly invalid or unsupported version as legacy.

For Knowledge, existing `register(registry)` files retain the current discovery,
registration, extraction, and activation behavior. Do not silently disable
existing plugins by imposing new enablement defaults. Capture their provenance
where feasible without changing the registry call signatures. Keep legacy
load-order replacement semantics until a caller chooses the versioned registry.
A partially registering legacy plugin is not promised atomic rollback by this
adapter; its current behavior is part of the compatibility baseline.

Versioned plugin registration uses an isolated candidate registry: validate all
descriptors/contracts before publishing bindings. A failed registration leaves
the active versioned bindings unchanged. Legacy and versioned bindings cannot
silently overwrite each other; their precedence must be explicitly configured.

## Deterministic capability selection

An internal binding maps a host capability/format key to a configured plugin ID
and supported operation version. Resolve bindings when composing the host, then
validate them before invoking a plugin:

1. An explicit binding must name an enabled, compatible provider of that key.
   Otherwise the key is unavailable with a diagnostic result.
2. Without an explicit binding, a single eligible provider can be selected.
3. With multiple eligible providers, mark that key ambiguous and do not invoke
   any of them. Other unambiguous capabilities remain available.
4. A fallback is permitted only through an explicitly configured ordered list
   of compatible providers, with recorded failure/staleness semantics.

Selection does not depend on directory iteration, package-version ordering, or
LLM-generated endpoint addresses. The binding is internal configuration for
the prototype; no new public route or environment variable is required.

## Limits, verification, and rollback

Keep existing limits for legacy Feeds. Define positive timeout and output limits
for each new invocation, honoring cancellation and the caller's remaining
deadline. Pass only declared configuration to the plugin. Verify typed output
before publishing it to a host or workspace. No plugin reads another capability's
database or receives raw access to the task workspace.

Baseline and implementation tests cover old manifest/registry fixtures, explicit
and omitted versions, disabled/incompatible plugins, collisions, configured
fallback, failure after partial registration, malformed output, hangs, and
process failure. Replacing the experiment extractor must leave router and
workflow policy unchanged. Execution isolation applies to the new subprocess
runtime, not retroactively to legacy in-process code.

Rollback removes the new adapter/bindings from composition and keeps the existing
loader path and plugin files. No existing plugin files are rewritten in place.
Host directories remain runtime input outside the public repository; only generic
fixtures are committed when implementation begins.
