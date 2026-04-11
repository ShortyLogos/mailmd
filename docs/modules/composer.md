---
title: Composer Module
last-updated: 2026-04-10
areas: [internal/ui/composer/composer.go, internal/markdown]
---

# Composer Module

Handles the editing → preview → send lifecycle after the compose dialog collects metadata.

## How It Works

### Two-Phase Flow

1. **Editing** — external editor opens with body content as a temp `.md` file. On editor exit, content is read back.
2. **Preview** — rendered markdown shown in a viewport. User can send (`y`), re-edit body (`e`), edit headers (`H`), browser preview (`P`), or cancel (`esc`).

### Metadata Mode

The composer has two initialization paths:

- **`NewWithMetadata()`** — used by the compose dialog. Receives To/CC/Subject as structured fields. Editor opens with body only (no frontmatter). This is the primary path going forward.
- **`New()` / `NewDraftEdit()`** — legacy path where metadata is embedded as YAML frontmatter in the editor file. Slated for deprecation.

In metadata mode, `editorDoneMsg` builds `ComposeData` directly from the stored fields + editor content, skipping frontmatter parsing entirely.

### Undo-Send

Send doesn't go to Gmail immediately. The composer emits `QueueSendMsg`, which `App` intercepts:
1. App stores the pending message and starts a 5-second countdown
2. Status bar shows countdown with U-to-cancel hint
3. If countdown expires, `App` calls `client.SendMessage()` (or `ReplyMessage()` for threads)
4. If user presses U, `App` restores the `pendingComposer` and re-enters preview phase

### Key Files

- `internal/ui/composer/composer.go` — `Model`, editor launch, preview rendering, send/cancel
- `internal/markdown/convert.go` — `Convert()` (md→HTML), `ConvertPlain()` (md→plain text)
- `internal/markdown/frontmatter.go` — `ParseCompose()` for legacy frontmatter mode
- `internal/ui/app.go` — undo-send state machine (`pendingSend`, `undoCountdown`, `pendingComposer`)

## Gotchas

- **Undo state lives in App, not Composer** — the composer emits `QueueSendMsg` and is done. App owns the countdown timer and the preserved composer snapshot for undo.
- **Draft save on cancel** — pressing Esc in preview auto-saves a draft via `SaveDraftMsg` if there's any content. This contacts the Gmail API, so it's not instant.
