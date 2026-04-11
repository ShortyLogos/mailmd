---
title: Compose Dialog Pattern
last-updated: 2026-04-10
areas: [internal/ui/app.go, internal/ui/common/messages.go]
---

# Compose Dialog Pattern

Modal overlay for collecting email metadata (recipients, subject, attachments) before opening the external editor. Replaces the previous frontmatter-in-editor approach which was tedious and couldn't support features like contact autocomplete.

## Decision / Rationale

Metadata was originally embedded as YAML frontmatter in the editor file. This was dropped in favor of an in-client dialog because:
- **Autocomplete** — recipient suggestions require TUI integration; impossible inside a raw text editor
- **Structured input** — file-path completion for attachments, field-by-field validation
- **CLI pre-fill** — `mailmd compose --to ... --subject ...` maps directly to dialog fields via `common.ComposeMsg`

The legacy frontmatter path still exists but is slated for deprecation.

## How It Works

The dialog is a modal overlay rendered by `App` (not a separate screen). It captures metadata, then hands off to the `composer.Model` which opens the external editor for body-only editing.

### Field Navigation

- Four fields in order: To → CC → Subject → Attachments
- Tab/Shift+Tab cycles fields
- Enter on To/CC adds the current input to a chip list; on Subject or Attachments, advances to next field
- Backspace on empty input removes the last chip
- Esc cancels (saves draft if content exists)

### Autocomplete

- Fires on To and CC fields using the contact cache (`contacts.All()`)
- Suggestions filtered by prefix match, sorted most-recently-used first
- Up/Down navigates suggestions, Enter selects

### Entry Points

All paths converge on `App.openComposeDialog()`:
- **New compose** — `c` key from inbox → empty dialog
- **Reply/Forward** — `r`/`f` from reader → pre-filled To, Subject, quoted body
- **Edit draft** — `e` from Drafts folder → pre-filled from draft content
- **Edit headers** — `H` from preview → re-opens dialog with current metadata
- **CLI** — `mailmd compose --to ...` → `common.ComposeMsg` → dialog

### Key Files

- `internal/ui/app.go` — `openComposeDialog()`, dialog state fields (`composeTo`, `composeField`, etc.), dialog rendering and key handling
- `internal/ui/common/messages.go` — `ComposeMsg`, `EditHeadersMsg`
- `internal/contacts/contacts.go` — autocomplete data source

## Gotchas

- **Dialog is App state, not a sub-model** — unlike inbox/reader/composer which are separate Bubble Tea models, the compose dialog lives as fields on `App`. This keeps message routing simple but means dialog logic is spread across `app.go`.
- **Undo re-opens preview, not dialog** — pressing U during undo-send restores the `composer.Model` (preview phase), not the compose dialog.
