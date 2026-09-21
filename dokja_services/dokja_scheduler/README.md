# Dokja Scheduler

Recurring scheduler service for Dokja.

Current jobs:

- `meme.pool.refresh` every `45m` by default
- `meme.dispatch.scheduled` every `6h` by default

Configuration:

- `DOKJA_SCHEDULER_EVENT_ENDPOINT`
- `DOKJA_SCHEDULER_TOPIC`
- `DOKJA_SCHEDULER_MEME_REFRESH_INTERVAL`
- `DOKJA_SCHEDULER_MEME_DISPATCH_INTERVAL`
- `DOKJA_SCHEDULER_MEME_BOOTSTRAP_GRACE_PERIOD`
- `DOKJA_SCHEDULER_MEME_REFRESH_MAX_ITEMS`
- `DOKJA_SCHEDULER_MEME_DISPATCH_LIMIT`

Notes:

- The refresh job is asynchronous and only emits `meme.pool.refresh`.
- The dispatch job is asynchronous and emits `meme.dispatch.scheduled`.
- The orchestrator handles scheduled meme delivery and chooses the interface/channel.
- On startup, the scheduler runs an initial refresh, waits for the bootstrap grace period, and only then emits the first scheduled dispatch.

## Operator control

The scheduler only publishes, so it is controlled through the orchestrator:

- `DOKJA_STATE_FILE` points at the state file the orchestrator writes. The scheduler re-reads it every 5 s
  for per-job `interval` overrides (1 minute to 30 days, anything else ignored) and restarts a job's
  countdown when its interval changes. Pausing a job is enforced by the orchestrator, so the scheduler still
  ticks and its startup run still happens; the orchestrator skips it.
- It announces its jobs (`scheduler.jobs.announce`: interval, last and next fire) at startup, after every
  run and about once a minute, so the orchestrator and the TUI can show the schedule.
