---
title: CLI Subcommands
last-updated: 2026-04-13
areas: [cmd/mailmd/main.go]
---

# CLI Subcommands

mailmd exposes subcommands for composing and drafting emails outside the interactive TUI, enabling scripting and LLM integration.

## Global Flags

### `-a` / `--account <name>`

Select which account to launch with. Matches against the `name` or `email` fields in config (case-insensitive). Available on the bare `mailmd` command and `compose` subcommand.

```bash
mailmd -a personal
mailmd --account work@company.com
mailmd compose -a work --to alice@example.com
```

When omitted, uses the last-used account (`LastAccount` in config). If no last account is set, defaults to the first configured account. If the account is not found, exits with an error listing available accounts.

Resolved in `initClient()` via the `accountFilter` parameter.

## How It Works

### `mailmd compose`

Opens the TUI with the compose dialog pre-filled. Flags map directly to dialog fields:
- `--to`, `--cc` (repeatable), `--subject`, `--body`, `--body-file`
- `-a` / `--account` — select account (parsed via `parseComposeFlagsWithAccount()`)
- Constructs a `common.ComposeMsg` and passes it as `InitialCompose` to the App
- App emits it as a tea.Msg on `Init()`, triggering the compose dialog

### `mailmd draft`

Creates a Gmail draft and exits without opening the TUI:
- Same flags as compose, plus `--open` (to open TUI instead of headless draft)
- Body can be piped via stdin (detected via `os.Stdin.Stat()`)
- Markdown body is converted to HTML+plain text, then sent to `client.CreateDraft()`
- Designed for LLM integration: generate markdown → `mailmd draft --to ... --body-file /tmp/draft.md`

### `mailmd help [topic]`

Prints contextual help. Topics: `compose`, `draft`, `keys` (full keybindings reference), `config`.

### Key Files

- `cmd/mailmd/main.go` — `run()` dispatch, `runCompose()`, `runDraft()`, `printHelp()`, `parseComposeFlags()`, `parseComposeFlagsWithAccount()`
- `internal/ui/common/messages.go` — `ComposeMsg` struct

## Gotchas

- **`draft` authenticates headlessly** — it calls `initClient()` which may trigger a browser OAuth flow on first run. Not suitable for fully unattended CI without pre-existing tokens.
- **stdin detection** — body is read from stdin only when no `--body` or `--body-file` is given AND stdin is not a terminal. Piping empty input produces no body, not an error.
- **account matching is case-insensitive** — `--account Personal` matches config `name = "personal"`.
