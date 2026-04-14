# mailmd

A terminal email client for Gmail where you compose in markdown.

Write emails in your favorite editor using markdown syntax, preview them rendered in the terminal or browser, and send them as properly formatted HTML — all without leaving the terminal.

Built with Go and the [Charm](https://charm.sh) ecosystem (Bubble Tea, Glamour, Lipgloss).

## Why

- You already write in markdown. Your emails should be too.
- LLMs output markdown. Copy their output straight into an email.
- No more web-based markdown converters, no more copy-pasting between tools.
- Gmail's web UI is slow. A terminal client with the Gmail API is fast.

## Features

**Compose**
- Write emails in markdown using your `$EDITOR` (Neovim, Zed, Helix, etc.)
- Emails sent as `multipart/alternative` — recipients see formatted HTML, plain text fallback included
- Terminal preview (Glamour) and browser preview for pixel-perfect verification
- Code blocks with syntax highlighting via Chroma

**Inbox**
- Full inbox experience — browse folders (Inbox, Drafts, Sent, Trash)
- Gmail API for speed (not IMAP)
- Background sync with per-folder caching
- Search using Gmail's full query syntax (`from:`, `subject:`, `has:attachment`, etc.)
- Multi-select with batch actions (trash, delete, restore)
- Line numbers with jump-to for quick navigation

**Reader**
- Email body rendered with Glamour
- Attachments listed with one-key open (`1`-`9`)
- Open all images at once (`I`) with concurrent downloads
- Reply (`r`) and forward (`f`) directly from the reader

**Navigation**
- Vim-style keybindings (`j/k`, `/` for search, `esc` to go back)
- Arrow keys (`right` to open, `left` to go back)
- Mouse support (scroll, click to select in inbox)
- Native text selection in the reader (mouse capture disabled)
- Folder-specific keybinds (permanent delete in Trash, restore with `u`)

**Security**
- OAuth2 browser sign-in (no passwords stored)
- Tokens at `0600` permissions, never logged
- No telemetry, no analytics, no external services beyond Gmail API
- "Bring your own credentials" option

**Multi-account**
- Multiple Gmail accounts with in-app switcher (`S`)
- Launch directly into an account: `mailmd -a personal` or `mailmd --account work@company.com`
- Signatures per account with default selection

## Install

### From source

```bash
git clone https://github.com/youruser/mailmd
cd mailmd
make build
```

### Homebrew

```bash
brew install youruser/tap/mailmd
```

## Setup

mailmd uses the Gmail API with OAuth2. You need Google Cloud credentials:

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project
3. Enable the **Gmail API**
4. Go to **APIs & Services > OAuth consent screen**, select External, fill in app name and email
5. Under **Test users**, add your Gmail address
6. Go to **Credentials > Create Credentials > OAuth client ID** (Desktop application)
7. Set environment variables:

```bash
export MAILMD_CLIENT_ID="your-client-id"
export MAILMD_CLIENT_SECRET="your-client-secret"
```

8. Run `mailmd` — your browser opens for authentication
9. After consent, the TUI launches with your inbox

## Usage

```bash
mailmd                              # open with last-used account
mailmd -a personal                  # open with account by name
mailmd --account work@company.com   # open with account by email
mailmd compose --to alice@example.com --subject "Hello"
mailmd draft --to bob@example.com --subject "Report" --body-file report.md
echo "Hello" | mailmd draft --to alice@example.com --subject "Hi"
mailmd help                         # show all commands
mailmd help keys                    # keybindings reference
```

The `-a` / `--account` flag works with `compose` too:

```bash
mailmd compose -a work --to team@company.com --subject "Update"
```

## Keybindings

### Inbox

| Key | Action |
|---|---|
| `j` / `k` | Navigate up/down |
| `o` / `Enter` / `right` | Open message |
| `c` | Compose new |
| `r` | Quick reply (from inbox) |
| `f` | Forward |
| `d` | Trash (or permanent delete in Trash/Drafts) |
| `u` | Restore from Trash |
| `Space` | Toggle select |
| `a` | Select all / deselect all |
| `p` | Toggle preview pane |
| `/` | Search (Gmail query syntax) |
| `1`-`9` | Jump to line number |
| `R` | Refresh |
| `Tab` / `Shift+Tab` | Switch folders |
| `Esc` | Clear selection, then clear search |
| `q` | Quit |

### Reader

| Key | Action |
|---|---|
| `j` / `k` | Scroll (5 lines) |
| `r` | Reply |
| `f` | Forward |
| `1`-`9` | Open attachment |
| `I` | Open all images |
| `Esc` / `left` | Back to inbox |
| `q` | Quit |

### Composer

| Key | Action |
|---|---|
| `y` | Send |
| `e` | Edit again |
| `P` | Browser preview |
| `Esc` | Cancel |

## Compose format

When you compose or reply, your `$EDITOR` opens with:

```
---
to: alice@example.com
subject: Meeting tomorrow
---

Hey Alice,

Are we still on for **tomorrow**?

- Bring the slides
- Don't forget coffee
```

The YAML-like frontmatter sets the headers. Everything below `---` is your markdown body.

## Configuration

Config lives at `~/.config/mailmd/config.toml`:

```toml
[general]
editor = ""         # defaults to $EDITOR, then vi
theme = "default"

[keybindings]
compose = "c"
reply = "r"
forward = "f"
trash = "d"

[preview]
browser = "default" # or "firefox", "chromium", etc.
```

Data locations:
- `~/.config/mailmd/config.toml` — user config
- `~/.config/mailmd/tokens.json` — OAuth2 tokens
- `~/.cache/mailmd/drafts/` — auto-saved drafts

## Tech stack

- **Go** with the [Charm](https://charm.sh) ecosystem
- **Bubble Tea** — TUI framework
- **Glamour** — terminal markdown rendering
- **Lipgloss** — terminal styling
- **Goldmark** — markdown to HTML conversion
- **Gmail API** — `google.golang.org/api/gmail/v1`

## License

MIT
