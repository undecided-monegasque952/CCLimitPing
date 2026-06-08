<p align="center">
  <img src="assets/icon.png" alt="CCLimitPing icon" width="160">
</p>

# CCLimitPing (`limitping`)

**English** | [中文](README.zh-CN.md)

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![CI](https://github.com/wavever/CCLimitPing/actions/workflows/ci.yml/badge.svg)](https://github.com/wavever/CCLimitPing/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/wavever/CCLimitPing?include_prereleases&sort=semver)](https://github.com/wavever/CCLimitPing/releases)
![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)

Keep your **Claude Code** and **Codex** rate-limit windows back-to-back.

These providers bill on a **5-hour rolling window** (plus a weekly cap), and the
5h window **starts on your first message**. If you don't send anything right when
a window resets, that gap is wasted — the next window only starts whenever you
happen to use the tool again, drifting out of sync with your day.

`limitping` watches each provider and, **the moment a 5h window resets, sends one
minimal message to start the next window immediately** — so your windows stay
continuous and predictable.

```
claude  ✓ pinged (6.6s)
codex   ✓ pinged (13.6s, 16,862 tok (in 16,814 / out 48), $0.0098)
```

## Highlights

- Keeps 5-hour provider windows continuous instead of letting idle gaps drift
  your schedule.
- Reads usage from zero-quota endpoints and triggers windows through the
  official provider tools.
- Supports Claude Code and Codex.
- Detects an actively running Claude/Codex session (via CLI hooks) and defers its
  ping so it never competes with your own turn.
- Includes dry-run modes, weekly-limit guards, reset buffers, local config, and
  no telemetry.

## Quick start

```sh
curl -fsSL https://raw.githubusercontent.com/wavever/CCLimitPing/main/install.sh | sh
limitping config init
limitping ping --dry-run
limitping status
limitping watch                # foreground (Ctrl-C to stop)
# ...or run it in the background, freeing your terminal:
limitping bg start
limitping bg status
```

Use `limitping ping --dry-run` or `limitping watch --dry-run` first if you want
to inspect what would happen without consuming provider quota.

## Supported providers

| Provider | Read usage (zero-quota) | Trigger | Auth |
|---|---|---|---|
| **Claude Code** | `…/api/oauth/usage` | interactive Claude Code CLI | OAuth (Keychain / `~/.claude`) |
| **Codex** | `…/backend-api/wham/usage` | `codex exec` | OAuth (`~/.codex/auth.json`) |

## How it works

Two cleanly separated jobs:

| Job | Mechanism | Cost |
|-----|-----------|------|
| **Trigger** a new window | the official CLI (interactive Claude Code / `codex exec`) | a tiny slice of quota (this is the point) |
| **Read** usage & reset times | zero-quota usage endpoints (the same ones CodexBar / community plugins use) | none — never starts a window |

When `watch` sees a 5h window has reset, it first checks whether a Claude/Codex
session is actively mid-turn. If one is, `limitping` waits and re-reads usage
instead of sending its own ping, because that session's next model request will
start the new window naturally. This check relies on the
[CLI hooks](#active-session-detection-hooks) (installed automatically by the
install script); without them, `limitping` skips the check and pings as soon as
the window resets.

- **Claude**: reads `GET https://api.anthropic.com/api/oauth/usage` using the
  OAuth token from the macOS Keychain (`Claude Code-credentials`) or
  `~/.claude/.credentials.json`. Triggering uses a TTY-backed interactive
  `claude "<prompt>"` session, so it continues to start the Claude
  subscription-backed window after the headless print command moves to Agent
  SDK/API credits.
- **Codex**: reads `GET https://chatgpt.com/backend-api/wham/usage` using the
  OAuth token from `~/.codex/auth.json`.

Claude/Codex tokens are reused from the official tools (no separate login) and
refreshed on 401.

## Install

`limitping` ships as a single self-contained binary — **no Go required**.

**One-line script** (macOS / Linux):

```sh
curl -fsSL https://raw.githubusercontent.com/wavever/CCLimitPing/main/install.sh | sh
```

Downloads the right prebuilt binary from the
[latest release](https://github.com/wavever/CCLimitPing/releases/latest) into
`/usr/local/bin` (or `~/.local/bin`). Override with `LIMITPING_INSTALL_DIR`.

**Upgrade** — replace the installed binary with the latest release:

```sh
limitping upgrade
```

Aliases: `limitping up`, `limitping update`.

**Uninstall** — remove the installed binary plus config/cache:

```sh
limitping uninstall
```

Aliases: `limitping rm`, `limitping remove`.

Use `limitping uninstall --keep-config` to preserve `~/.config/limitping` (or
`$XDG_CONFIG_HOME/limitping`).

**Manual download** — grab the archive for your platform from the
[Releases](https://github.com/wavever/CCLimitPing/releases) page (`.tar.gz` for
macOS/Linux, `.zip` for Windows):

```sh
tar -xzf limitping_darwin_arm64.tar.gz
sudo mv limitping /usr/local/bin/
```

**Homebrew** (macOS / Linux) — `brew install wavever/tap/limitping`
_(works once the Homebrew tap is set up — see `.goreleaser.yaml`)._

**From source** (developers, needs Go 1.25+):

```sh
go install github.com/wavever/CCLimitPing/cmd/limitping@latest
# or, from a clone:
go build -o bin/limitping ./cmd/limitping
```

Each provider you enable needs its own credentials: the `claude` / `codex` CLIs
logged in.

## Usage

```sh
limitping config init          # write ~/.config/limitping/config.toml
limitping status               # show 5h/weekly % + reset countdowns (alias: s)
limitping status -v            # also print raw JSON
limitping ping                 # trigger all enabled providers now (alias: p)
limitping ping claude          # Claude only
limitping ping codex           # Codex only
limitping ping --dry-run       # show the commands without sending
limitping watch                # foreground daemon: ping each window at reset (alias: w)
limitping watch claude         # watch only one provider (claude|codex)
limitping watch --dry-run      # log when pings would fire, without sending
limitping bg start             # run watch in the background, freeing the terminal
limitping bg status            # running? + each watched provider's usage (alias: limitping bg)
limitping bg logs -f           # follow the background watcher's log
limitping bg stop              # stop the background watcher
limitping hooks install        # install active-session detection hooks (claude|codex|all)
limitping hooks uninstall      # remove those hooks
limitping version              # print the version (aliases: v, ver)
limitping upgrade              # update to the latest GitHub release (aliases: up, update)
limitping uninstall            # remove limitping plus config/cache (aliases: rm, remove)
```

Short aliases are also available for config commands: `limitping c i` for
`config init` and `limitping c p` for `config path`.

### Command aliases

`limitping --help` lists aliases inline, for example `ping, p`.

| Command | Aliases |
| --- | --- |
| `status` | `s`, `stat` |
| `ping` | `p` |
| `watch` | `w` |
| `background` | `bg` |
| `config` | `c`, `cfg` |
| `config init` | `c i` |
| `config path` | `c p` |
| `version` | `v`, `ver` |
| `upgrade` | `up`, `update` |
| `uninstall` | `rm`, `remove` |

`ping` shows the exact command, a live timer (a spinner on a terminal), the
**token usage** the ping consumed where available (parsed from `codex --json`),
and a **USD cost** where available:

```
claude  → claude --model haiku .
claude  ✓ pinged (6.6s)
codex   → codex exec --skip-git-repo-check --json -c model_reasoning_effort=low -m gpt-5.4-mini ok
codex   ✓ pinged (13.6s, 16,862 tok (in 16,814 / out 48), $0.0098)
```

Cost sources:
- **Claude** interactive mode does not expose per-invocation machine-readable
  usage/cost, so no token/cost suffix is shown.
- **Codex** (subscription) doesn't return a USD cost, so — like CodexBar/ccusage
  — we derive the equivalent API-rate cost from the
  [LiteLLM pricing dataset](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json)
  (`cost = non-cached-input × input + cached-input × cache-read + output × output`).
  The dataset is cached at `~/.config/limitping/litellm_prices.json` (24h TTL),
  with model-alias/date-suffix fallbacks. Requires `[codex].model` to be set so
  the rate can be looked up.

Claude triggering still consumes a small amount of Claude subscription quota,
but the interactive CLI does not expose the exact per-ping token count.

Example `status`:

```
claude
  5h     [█████░░░░░]  51.0%  resets in 3h14m    (Sun 00:10)
  weekly [█████░░░░░]  54.0%  resets in 7h04m    (Sun 04:00)

codex (plus)
  5h     [██░░░░░░░░]  24.0%  resets in 3h15m    (Sun 00:11)
  weekly [████░░░░░░]  37.0%  resets in 111h57m  (Thu 12:53)
```

## Configuration

`~/.config/limitping/config.toml` (honors `$XDG_CONFIG_HOME`):

```toml
weekly_threshold = 0.99   # skip pinging when weekly usage >= this (0..1), until weekly reset
reset_buffer     = "10s"  # wait this long after a reset before pinging (ensures rollover)
notify           = true   # macOS notifications on ping/skip/failure

[claude]
enabled    = true
prompt     = "."
model      = "haiku"      # cheapest tier; triggering doesn't need a SOTA model
extra_args = []           # extra Claude CLI args; print/headless-only flags are ignored
align_start = ""          # optional RFC3339 anchor for the first window; empty = start ASAP

[codex]
enabled          = true
prompt           = "ok"
model            = "gpt-5.4-mini"  # cheapest Codex model for triggering
reasoning_effort = "low"  # "minimal" is rejected when web_search/image_gen tools are enabled
extra_args       = []
align_start      = ""
```

Top-level keys:

- **`weekly_threshold`** — when the weekly window is at/above this, `watch` stops
  pinging and waits for the weekly reset (unless usable credits exist).
- **`reset_buffer`** — how long to wait after a window's reset time before
  pinging, so the window has definitely rolled over.
- **`align_start`** (per provider) — pin the phase of your windows: set to a
  future RFC3339 time to delay the very first ping until then; afterwards windows
  chain automatically every ~5h.

### Why a cheap model

Triggering a window doesn't depend on the model — **any** billable request starts
the 5h clock — so the ping uses each provider's cheapest model to eat the least of
your budget:

- **Claude → `haiku`**: also avoids the separate weekly Opus bucket.
- **Codex → `gpt-5.4-mini`**: the mini variant (see `~/.codex/models_cache.json`
  for what your plan offers).

Claude/Codex don't expose per-model prices at runtime (Anthropic's local cost
cache is empty; Codex's model cache has no price field), so the cheapest model is
a sensible default rather than a live price lookup. Override `model` per provider
if you prefer.

### Active-session detection (hooks)

At a window reset, `watch` avoids pinging while you're actively working — that
turn would start the next window on its own. This relies on **CLI hooks**, which
the install script sets up for you. If they aren't installed, `limitping` skips
the check entirely and pings right at reset (it never guesses from the process
list).

The install script runs this automatically; to (re)install manually:

```sh
limitping hooks install        # both providers (or: limitping hooks install claude)
```

This registers limitping's hooks in `~/.claude/settings.json` and
`~/.codex/hooks.json` (your existing settings are preserved; a `.bak` backup is
written). The hooks invoke the hidden `limitping hook <provider>` command on
`UserPromptSubmit` / `PreToolUse` / `PostToolUse` / `Stop` (Claude also
`SessionEnd`) to record whether a session is mid-turn under
`~/.config/limitping/activity/`.

> [!NOTE]
> Claude Code loads its hooks automatically — nothing to do there. **Codex**
> gates custom command hooks behind a one-time trust step: run `/hooks` inside
> Codex once to enable them. Remove everything later with
> `limitping hooks uninstall` (also done automatically by `limitping uninstall`).

## Run `watch` in the background

`watch` runs in the foreground. To free your terminal, run it as a detached
background process with the built-in `bg` command:

```sh
limitping bg start          # start watch detached from the terminal
limitping bg status         # running? pid, uptime, log + each provider's usage (alias: limitping bg)
limitping bg logs -f        # follow the watcher's log (-n N for last N lines)
limitping bg stop           # stop it
```

`bg start` takes the same optional `[provider]` argument and `--dry-run` flag as
`watch`. Only one background watcher runs at a time, and its output is written to
`~/.config/limitping/bg.log` (honors `$XDG_CONFIG_HOME`). The process detaches
into its own session, so it survives the shell closing — but it does **not**
restart on reboot.

For **start-at-login** on macOS, use a `launchd` agent instead. Create
`~/Library/LaunchAgents/com.limitping.watch.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.limitping.watch</string>
  <key>ProgramArguments</key>
  <array>
    <string>/ABSOLUTE/PATH/TO/limitping</string>
    <string>watch</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/tmp/limitping.log</string>
  <key>StandardErrorPath</key><string>/tmp/limitping.err</string>
</dict>
</plist>
```

```sh
launchctl load ~/Library/LaunchAgents/com.limitping.watch.plist
```

## Cost & caveats

- See [PRIVACY.md](PRIVACY.md) for local data handling and network behavior.
- See [SECURITY.md](SECURITY.md) for vulnerability reporting and credential
  handling notes.
- Triggering **consumes a little quota** (~one ping per 5h ≈ 33/week). The ping
  uses a minimal prompt and low reasoning, so the cost is tiny but non-zero.
- The **usage endpoints are unofficial** and could change; they're read-only and
  isolated per provider for easy patching.
- macOS-first: Keychain reads and notifications are macOS-only. Codex `auth.json`
  is cross-platform; Claude on Linux uses `~/.claude/.credentials.json`;
  notifications are a no-op off macOS.

## Layout

```
cmd/limitping            CLI entry
internal/config          TOML config
internal/usage           normalized usage model
internal/auth            Claude (Keychain) + Codex (auth.json) tokens
internal/provider        per-provider ReadUsage (endpoint) + Trigger (CLI)
internal/activity        hook-based active-session state (shared by the hook cmd + scheduler)
internal/pricing         LiteLLM-based USD cost lookup (Codex)
internal/scheduler       the watch engine (sleep-until-reset, weekly-respect, backoff)
internal/notify          macOS osascript notifications
internal/cli             cobra commands: status, ping, watch, background, config, hooks, upgrade, uninstall, version
```

## Contributing

Issues and PRs are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) and
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Before submitting:

```sh
gofmt -l .        # should print nothing
go build ./...
go vet ./...
go test ./...
```

Providers are isolated in `internal/provider` (one file each) with a small
`Provider` interface (`ReadUsage` + `Trigger`), so adding a new provider is
mostly a self-contained file plus wiring in `internal/cli` and `internal/config`.

**Releasing** is automated: push a tag and GitHub Actions runs GoReleaser to
build the cross-platform binaries and publish a Release.

```sh
git tag v0.2.0 && git push origin v0.2.0
```

## License

[MIT](LICENSE) © wavever
