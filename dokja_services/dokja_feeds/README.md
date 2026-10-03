# dokja_feeds

Follows the sources the owner chooses and keeps their latest items in memory, so the digest can
show them. The repository ships **the mechanism only**: no source, site or provider is built in.
What to follow is a plugin, kept outside the repository.

- A plugin is a directory with a `plugin.json` manifest and a program. The service reads the
  manifest without running anything, so a plugin that is disabled, broken or unknown is still
  listed with the reason.
- Each enabled plugin runs on its own schedule. A request only reads the last good result from
  memory, so a slow or hung plugin never delays an answer.
- A failed run keeps the previous items and says why it failed; one failing plugin does not hide
  the others.
- Nothing starts by itself: a plugin runs only if its manifest says `"enabled": true`.

## Events (ZeroMQ REQ/REP, port 5563)

The envelope is the one the memory and knowledge services use: `{"type", "payload"}` in,
`{"status", "result" | "message"}` out. Both events read memory and take no payload.

| event | result |
| --- | --- |
| `feeds.status` | `configured`, `directory_error`, `plugins[]`, and the counts `enabled`, `ok`, `failed`, `pending`, `disabled`, `invalid`, `items` |
| `feeds.items` | `items[]` (at most 1000), `total`, `updated` |

A `plugins[]` row has `id`, `name`, `state` (`ok`, `failed`, `pending`, `disabled` or `invalid`),
`running`, `enabled`, `interval_seconds`, `items`, `skipped`, `last_run`, `last_ok` and
`last_error`. An `items[]` entry has `id`, `plugin`, `title`, `summary`, `url`, `published` (RFC 3339
UTC, or empty) and `source`. `last_error` is a short fixed phrase such as `exit status 2` or
`timed out after 30s`; what a plugin writes to stderr never appears in a reply.

## Writing a plugin

One directory per plugin under `FEEDS_PLUGINS_DIR`, named like its `id`:

```
plugins/
  my-source/
    plugin.json
    run.sh
```

`plugin.json` (unknown fields are refused, so a typo is reported instead of ignored):

| field | meaning | default |
| --- | --- | --- |
| `id` | lowercase letters, digits, `-` or `_`, at most 40; must equal the directory name | required |
| `category` | `"feed"` | required |
| `command` | the program and its arguments, run without a shell; the program is either on `PATH` or a file inside the plugin directory | required |
| `enabled` | `true` to run it | `false` |
| `name` | what to call it when an item names no source | the `id` |
| `interval` | time between runs, `1m` to `24h` | `15m` |
| `timeout` | how long one run may take, `1s` to `5m` | `30s` |
| `env` | names of the environment variables the plugin receives, taken from the service's own environment | none |
| `max_items` | most items kept from one run, 1 to 200 | `50` |

The program prints one JSON object on **stdout** and exits with status 0. Anything else it wants
to say goes to **stderr**, which only reaches the service log:

```json
{"items": [
  {"id": "1", "title": "An example title", "summary": "Some text", "url": "https://example.com/1",
   "published": "2026-10-01T08:00:00Z", "source": "Example"}
]}
```

Only `title` is required. Every field is a string. Without an `id` the service derives a stable
one from the address and the title. A `published` that is not RFC 3339, or lies more than an hour
in the future, is treated as unknown.

### What the service checks

Plugin output comes from the outside, so it is bounded and cleaned before it is kept:

- at most 1 MiB of output per run, and `max_items` items; an item that cannot be used is skipped
  and counted in `skipped`, the rest stays;
- text is made one line of valid UTF-8 without control characters or the characters that reorder
  what is displayed, and cut to 300 characters (title), 1000 (summary) and 80 (source);
- `url` is kept only if it is an absolute `http` or `https` address with a host and no credentials.

### What the service does to a plugin

A plugin is code you wrote, but it may still be slow, wrong or hung, so each run is a child
process that:

- starts in its own directory with no input and only `PATH` plus the variables named in `env`
  (a secret reaches a plugin only by being listed there, by name; keep tokens in the service
  environment, never in a manifest);
- is stopped when `timeout` ends or the output passes the limit, together with everything it
  started;
- is never run while another run of the same plugin is in progress.

### Where plugins run

The plugin runs inside the service's container, so **its program must exist in that image**. The
image is `debian:bookworm-slim` plus the libraries the service needs: it has a POSIX shell (`sh`)
and the usual command-line tools, and no Python or other interpreter. For anything else, extend
the image, or ship a static binary in the plugin directory. A manifest whose program is not
installed shows as `invalid` with the program's name.

Mount the plugins directory read-only and keep it out of git. Changes to it are read when the
service starts.

A plugin that uses no network is in `plugins.example/`; it prints the items of a local JSON file.
It ships disabled.

## Variables

`FEEDS_SERVICE_ENDPOINT` (default `tcp://*:5563`), `FEEDS_PLUGINS_DIR` (empty means no plugins).

## Tests

The service links libzmq through cgo (`libzmq3-dev` and `pkg-config`), like the other Go services:

```sh
cd dokja_services/dokja_feeds && go test ./...
```
