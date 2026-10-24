# Local models with Ollama

Status: planned, not implemented.

## Why

The owner asked to ship Ollama and list its models, "instead of only one". In System → Providers,
Ollama showed `bge-m3:latest` and nothing else. There was no way to get a model to chat with from the
desktop.

## Current evidence

- **Already shipped.** Ollama runs as the `dokja-ollama` service in the lite and prod compose files.
  The runtime knows it as the module `embeddings`, marked `Internal`
  (`dokja_domain/dokja_runtime/runtime.go`):
  - it starts as a dependency of `embedding_init`, so whenever memory or knowledge starts;
  - it is never listed in System.
- **Discovery.** It runs `ollama list` in that container (`dokja_services/dokja_runtime/providers.go`
  and `compose.go`). It lists what is installed. `bge-m3` is the embedding model the stack pulls for
  itself, and it is not a chat model.
- **Chat.** It runs `ollama run <model>` in that container (`dokja_services/dokja_runtime/chat.go`).
  The desktop's "Use for chat" (#112) works for any installed model.
- **No download.** Nothing pulls a model. Installing one today means running `ollama pull` by hand
  inside the container.

## Intended behavior and boundaries

From System, the owner sees Ollama as the local model engine:
- whether it runs;
- the models it has, with their size;
- the option to download a model by name, with progress, and to remove one.

Nothing downloads without a real press and a native confirmation that names the model and warns that a
download can take gigabytes. One download runs at a time. The runtime controller owns the engine, as it
owns every module; the desktop only asks it.

## Proposal

1. **Show the engine.**
   - Keep `embeddings` internal for starting and stopping, since other modules depend on it.
   - Report its state and its models on the Providers row, under a plain name: "Ollama (local models)".
   - The row offers **Start** when it is stopped and nothing depends on it yet, which allows chatting
     without memory.
2. **Download and remove.**
   - New runtime actions:
     - `runtime.models.pull` with `{ model }`, model names bounded by a strict pattern;
     - `runtime.models.remove` with `{ model }`;
     - `runtime.models.status` reports a running download, read the way `runtime.modules` reports a
       busy module.
   - The controller runs `ollama pull` / `ollama rm` in the container and parses the progress lines.
   - A pull can be cancelled.
   - `bge-m3` cannot be removed while memory or knowledge need it.
3. **Desktop.**
   - Providers gains a **Download a model** field. The shell offers a short list of suggested chat models
     that the owner can edit, a progress line while the download runs, and the native confirmation
     before it starts.
   - Once a model is downloaded it appears with **Use for chat**.
   - The embedding model is marked as such and is not offered for chat.

## Compatibility

- New runtime actions and allow-list entries.
- No change to compose, volumes (the models already live in the `dokja-ollama` volume), environment,
  envelope or orchestrator routes.
- An old runtime controller answers that the action is unknown; the desktop then hides the download
  field.

## Phases (one PR each)

1. **Runtime:** the `runtime.models.*` actions with progress and cancellation, tested with a stand-in
   for `docker compose exec` that prints pull progress.
2. **Desktop:**
   - the engine row (state, models with sizes);
   - Start;
   - Download with confirmation and progress;
   - Remove;
   - the embedding model marked and not offered for chat.

## Risks and open questions

- **Disk and network.** Models are gigabytes. The confirmation shows the size when the registry gives
  it, and the download runs only on the owner's press.
- **Which models to suggest.** The suggestions are runtime input kept in the shell's settings, not
  hard-coded names tied to one vendor; the owner edits the list.
- **GPU or CPU.** The container's acceleration is the compose stack's concern. The plan does not change
  it.
- **Separate engine.** Should chat get its own Ollama instance, apart from embeddings? Not planned; one
  engine serves both unless load proves otherwise.
