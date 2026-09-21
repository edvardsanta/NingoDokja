# Secret and policy scanning

The repository is public. Every commit is scanned with [gitleaks](https://github.com/gitleaks/gitleaks)
for two kinds of problems:

- **Credentials**: API tokens, bot tokens, private keys and the like, using gitleaks' default rules.
- **Project policy**: things a public repository must not carry, because they tie it to a real place:
  Discord webhook URLs and bot tokens (gitleaks has no default rule for these), addresses of real
  third-party sites, Discord channel/guild/webhook ids, personal paths such as `/home/<user>/`, and
  the names of the sites, providers and events the project decided to stay away from.

A URL is an input the user supplies at run time, never a built-in default. Use reserved placeholders in
code, tests and docs: `example.com`, `*.example`, `localhost`, compose service names.

## Two configurations

| File | Rules |
| --- | --- |
| `.gitleaks.toml` | gitleaks' default credential rules |
| `.gitleaks-policy.toml` | the project's own rules, listed below |

They are separate on purpose. The default configuration has a long list of global stopwords (`home`,
`config`, `app-`, `admin`, `author`...) that silently drops any finding containing one of them. That is
reasonable when guessing at random secrets, but it would let `https://some-site.example/config` or
`/home/alice/` through a policy rule. The policy file does not extend the defaults, so nothing is
filtered that way.

Policy rules (`.gitleaks-policy.toml`): `dokja-discord-webhook`, `dokja-discord-bot-token`,
`dokja-external-url`, `dokja-discord-id`, `dokja-personal-path`, `dokja-avoided-term`.

## Where it runs

1. **Pre-commit**, on the staged changes, before the commit exists.
2. **CI** (`.github/workflows/gitleaks.yml`), on every pull request whatever its base branch and on
   pushes to `main`. It scans the commits the change introduces.
3. **By hand**: `scripts/scan-secrets.sh` (see below).

Scanning is per commit, not per net diff. That is deliberate: history is public, so a value added in one
commit and removed in the next was still published. Fix it in the commit that introduced it.

## Install

Install `gitleaks` v8.28.0 or newer (the CI pins v8.28.0). Any of these works:

```sh
brew install gitleaks
go install github.com/zricethezav/gitleaks/v8@v8.28.0   # the module path that version declares
# or download a release binary: https://github.com/gitleaks/gitleaks/releases
```

For the pre-commit hooks:

```sh
pip install pre-commit      # or: pipx install pre-commit
pre-commit install
```

The pre-commit gitleaks hooks build gitleaks themselves, so the `gitleaks` binary is only needed for
`scripts/scan-secrets.sh`. This file's `.pre-commit-config.yaml` also runs the Python checks for
`dokja_lab`.

## Run it by hand

```sh
scripts/scan-secrets.sh              # commits not on origin/main yet
scripts/scan-secrets.sh HEAD~3..HEAD # a range
scripts/scan-secrets.sh --staged     # what is staged
scripts/scan-secrets.sh --all        # the whole history
pre-commit run gitleaks gitleaks-policy --all-files
```

Findings are printed with the value redacted. The exit code is non-zero when there is any finding.

## When it reports something

- **A real credential**: revoke it first, then remove it. Removing it from the code does not un-leak it.
- **A real host, id, path or avoided term**: replace it with a placeholder. If it is a default, make it an
  input instead.
- **A legitimate placeholder**: add a narrow entry to that rule's allowlist in `.gitleaks-policy.toml`
  (or `[allowlist]` in `.gitleaks.toml`) with a comment saying why. Do not disable a rule. Prefer fixing
  the fixture: an `*.example` host needs no exception.

If the value is already in a pushed commit, GitHub keeps force-pushed commits reachable by SHA; only
GitHub Support can purge them. Catch it before the first push.

## What it does not do

It matches patterns. It cannot tell whether a hostname is one the project should avoid unless the
pattern says so, and it does not read the meaning of what you wrote. The policy rules are a net for the
common mistakes, not a replacement for review.
