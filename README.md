# mailmd

A terminal email client for Gmail where you compose in markdown.

## Features

- **Markdown-first composition** — Write emails in your favorite editor using markdown
- **Dual preview** — Terminal preview (Glamour) + browser preview for pixel-perfect HTML
- **Gmail API** — Fast, native Gmail integration (not IMAP)
- **Vim-style keybindings** — Navigate your inbox without leaving the keyboard
- **Secure** — OAuth2 authentication, tokens stored locally at 0600, no telemetry

## Install

### Homebrew

```bash
brew install deric/tap/mailmd
```

### From source

```bash
git clone https://github.com/deric/mailmd
cd mailmd
make build
```

## Setup

mailmd uses the Gmail API with OAuth2. You need to create your own Google Cloud credentials:

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project
3. Enable the **Gmail API**
4. Create **OAuth 2.0 Client ID** (Desktop application)
5. Set environment variables:

```bash
export MAILMD_CLIENT_ID="your-client-id"
export MAILMD_CLIENT_SECRET="your-client-secret"
```

6. Run `mailmd` — it will open your browser for authentication

## Keybindings

| Key           | Action          |
|---------------|-----------------|
| `j/k`         | Navigate up/down|
| `o` / `Enter` | Open message    |
| `c`           | Compose new     |
| `r`           | Reply           |
| `f`           | Forward         |
| `d`           | Trash           |
| `p`           | Terminal preview|
| `P`           | Browser preview |
| `Tab`         | Next folder     |
| `Shift+Tab`   | Previous folder |
| `q`           | Quit            |

## Configuration

Config lives at `~/.config/mailmd/config.toml`:

```toml
[general]
editor = ""         # defaults to $EDITOR
theme = "default"

[keybindings]
compose = "c"
reply = "r"
forward = "f"
trash = "d"

[preview]
browser = "default"
```

## License

MIT
