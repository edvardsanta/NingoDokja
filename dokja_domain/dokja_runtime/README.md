# Runtime lifecycle policy

Runtime is an infrastructure domain, separate from conversational business
capabilities. It owns the fixed lite-stack module inventory, required dependency
order, state transitions, explicit root selection and shared dependency ownership.
It calls only an injected stateless backend; it contains no process execution,
filesystem, HTTP or container integration.

Selectable modules: orchestrator, Memory, Knowledge, Meme, Book, Feeds, Voice and
WhatsApp. Memory and Knowledge require Ollama plus successful embedding initialization;
application modules require the main orchestrator. Only an explicit WhatsApp selection starts its gateway. No selection starts Discord
or the scheduler. Their running containers, and running CLI/TUI instances,
are recognized as external consumers so an explicit stop cannot remove their
required core/voice dependencies.

New/read operations do not start containers. On acknowledges `starting`, applies
only its dependency closure, and reaches `ready` after backend readiness. A
previously running container without a controller readiness check is `running`,
not `ready`. Off acknowledges `stopping` and preserves data, borrowed dependencies
and shared consumers. Failures remain visible until explicit retry. Restarting a
normally stopped application container works even when its last exit code was 0;
only the one-shot embedding initializer treats successful completion as readiness.

The journal contains ownership and root selections, not credentials or domain
content. Writing ownership intent precedes creating containers. Journal failures
stop the operation before new containers are created. The daemon persists it
privately; restoring it never automatically starts modules. Lifecycle operations
are serialized so starts, stops and shared dependency cleanup cannot cross.
