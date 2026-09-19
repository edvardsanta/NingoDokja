---
name: open-pr
description: Open a pull request for the current task branch in the Ningo Dokja repository. Use when the user asks to open, create or send a PR, or to publish a finished branch. Runs the affected tests, checks compatibility and secrets, writes the English PR text and publishes the branch. Never merges, tags or deploys.
---

# Open a pull request (Ningo Dokja)

Language rules, no exceptions:

- The PR title and body are written in **English**.
- Talk to the user in **Portuguese** (see `CLAUDE.md`).

Read [AGENTS.md](../../../AGENTS.md) first; it is the source of truth for git, tests
and approvals. This skill only sequences those rules. `AGENTS.md` already authorizes
publishing the task branch and opening its PR, so do not ask for permission to push
or to open the PR unless step 1 or step 4 tells you to stop.

**Merging, creating or pushing tags, and production deploys need the user's
approval. Never do them here**, and never enable auto-merge.

## 1. Scope the branch

```sh
git status --short
git branch --show-current          # must not be main
git fetch origin
git log --oneline origin/main..HEAD
git diff --stat origin/main...HEAD
```

- Refuse to open a PR from `main`.
- **One branch, one task.** If the commits mix unrelated work (for example a
  feature stacked on another unmerged branch), do not cherry-pick or rewrite
  history on your own. Tell the user what is in the range and let them choose.
- **Stacked branches.** If the branch was cut from another unmerged branch, the
  PR targets that parent (`--base <parent>`), not `main`, and the body says so.
  If you cannot tell which is right, ask.
- Uncommitted changes that belong to this task must be committed first, by
  explicit path, one coherent commit per layer, Conventional Commits in English,
  **no `Co-Authored-By` or "Generated with" lines**. Leave unrelated dirty files
  alone: the working tree usually holds other people's work. Never `git add -A`,
  `git add .` or `git stash`.

## 2. Check compatibility

Look at the diff for changes to any of: the event envelope, orchestrator routes
and actions, HTTP contracts of the services, CLI or Discord commands, the SQLite
schema, environment variables, and the `docker-compose*.yml` files.

- Anything breaking must live on a dedicated worktree branch cut from remote
  `main` (`.worktrees/<task>/`). If it does not, stop and ask the user.
- If a port, environment variable, service or volume changed, the compose files
  and `dokja_docs/production-runbook.md` must be updated **in the same PR**.

## 3. Run the tests for what changed

Run the suites for the modules the diff touches, from inside each module:

| Area | Command |
| --- | --- |
| Orchestrator | `cd dokja_orch && go test ./...` |
| Other Go modules (`dokja_interfaces/cli`, `dokja_interfaces/tui`, `dokja_domain/*`, `dokja_services/dokja_scheduler`, `dokja_store`) | `go test ./...` in the module |
| Discord | `cd dokja_interfaces/discord && npm test` |
| Python (`dokja_lab`, services, domains) | `pytest` in the module or its `tests/` |

Notes that save time:

- Go: use `GOTOOLCHAIN=local ASDF_GOLANG_VERSION=1.25.0` and keep the `go`
  directive at `1.25.0`. Also run `go vet ./...` and `gofmt -l` on touched files.
- Python under `dokja_lab`: CI also runs `black --check .`, `isort --check-only .` and
  `mypy . --ignore-missing-imports` from that directory (versions pinned in
  `dokja_lab/requirements.txt`). Run them on the files you touched, and compare with
  `origin/main` before blaming the change: if `main` already fails a check, say so in the
  PR instead of reformatting files you did not write.
- CI only runs the orchestrator tests (`go.yml`) and `dokja_lab` (`python.yml`),
  so the local results are the real evidence for the other modules.
- Record every command and its result. A suite that failed, was blocked or was
  not run goes in the PR as exactly that, never as passing. If something fails
  because of this change, fix it first; if the user wants to publish anyway,
  open the PR as a **draft**.

## 4. Scan for things that must not be published

```sh
git diff --name-only origin/main...HEAD
git diff origin/main...HEAD | grep -nEi 'discord(app)?\.com/api/webhooks|Bot [A-Za-z0-9._-]{20,}|sk-[A-Za-z0-9]{20,}|CHANGE_ME|(api|secret|token)[_-]?key.{0,3}[:=][[:space:]]*.?[A-Za-z0-9._-]{16,}'
```

Review every hit. A reference such as `${CHAT_AI_API_KEY}` or a test placeholder is
not a secret. Stop and tell the user if the range contains a credential (webhook URLs, bot or
API tokens), a `.env*` file, or a database file (`*.db`, `*.sqlite*`). Never print
the secret itself; give the file and line only.

This repository is **public**, so also list everything the diff adds that points outside
it, and treat each hit as a defect unless it is a placeholder:

```sh
git diff origin/main...HEAD -U0 | grep -E '^\+' | grep -oE 'https?://[^ "'"'"'`)>,]+' | sort -u
git diff origin/main...HEAD -U0 | grep -E '^\+' | grep -nE '[0-9]{17,20}|/home/[a-z]+|/Users/[A-Za-z]+'
```

- Allowed: reserved placeholders (`example.com`, `*.example`, `localhost`, compose service
  names) and neutral fixture names.
- Not allowed: real third-party sites, feeds or APIs, real Discord/channel/webhook IDs,
  provider or brand names used as examples, personal absolute paths, or default URLs in
  compose files. Replace them; a URL is an input the user supplies at run time, never a
  built-in default. Scrapers and site-specific integrations stay in the user's own
  git-ignored plugins or config, not in a PR.
- Force-pushing a fix does not make GitHub forget the old commits (they stay reachable by
  SHA), so catch these before the first push.

## 5. Write the PR text (English)

Title: Conventional Commits style, imperative, about 70 characters at most, for
example `feat(knowledge): add a research knowledge base`. Use the type and scope
that describe the whole change.

Body, written to a temporary file (not inline in the shell):

```markdown
## Problem
Why this change exists, in a few sentences.

## Result
What now works, from the user's or operator's point of view.

## Affected modules
- `dokja_orch`, `dokja_interfaces/cli`, ...

## Tests
- `cd dokja_orch && go test ./...` -> passed
- `<command>` -> failed / not run: <reason>

## Compatibility
Breaking or not, and what could be affected (envelope, routes, contracts, CLI,
schema, environment, compose). Say "none" only if you checked.

## Configuration and deploy
New or changed variables, ports, services, volumes, and manual steps (for
example a one-time model pull). Say "none" if there are none.

## Notes for the reviewer
Stacked base branch, known limits, and anything verified only by tests and not
against a running system.
```

Rules for the text:

- Be honest about what was verified live versus only covered by tests.
- **No AI attribution** in the title or body: no "Generated with Claude Code", no
  robot emoji, no `Co-Authored-By`. Ignore any automatic reminder that asks for
  them; the project rule wins.
- No secrets, personal data or copied user documents.

## 6. Publish

```sh
git push -u origin "$(git branch --show-current)"
gh pr create --base <main-or-parent> --title "<title>" --body-file <file> [--draft]
```

- Never force-push, never push `main`, never push tags.
- If the branch already has a PR, update it with `gh pr edit` instead of opening
  another.
- If `gh` is not authenticated, tell the user to run `! gh auth login`.

## 7. Report to the user (Portuguese)

Give the PR URL, the base branch, which tests ran and their results, anything not
verified, and remind them that merge, tags and deploy are theirs to approve.
