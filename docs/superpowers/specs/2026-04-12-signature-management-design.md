# Signature Management & Auto-Import Design

**Date:** 2026-04-12
**Scope:** Multi-signature support per account, settings panel, Gmail import, compose dialog picker

## Overview

Currently each account has a single optional `signature` string in config.toml. This spec adds:
- Multiple named signatures per account with a default marker
- A settings panel (`,` key) to manage signatures (add/edit/delete/import/set default)
- Gmail signature auto-import via the SendAs API
- A signature selector field in the compose dialog

## Data Model

### Config Changes (`internal/config/config.go`)

Replace the single `Signature string` field on `Account` with a slice:

```go
type Signature struct {
    Name      string `toml:"name"`
    Body      string `toml:"body"`
    IsDefault bool   `toml:"is_default,omitempty"`
}

type Account struct {
    Name       string      `toml:"name"`
    Email      string      `toml:"email"`
    Signatures []Signature `toml:"signatures,omitempty"`
    // Deprecated: single Signature field migrated on load
}
```

### Migration

On config load, if the old `signature` field is non-empty and `signatures` is empty, convert:
- Create `Signatures: []Signature{{Name: "Default", Body: oldSig, IsDefault: true}}`
- Clear the old field
- Save config to persist the migration

### TOML Format

```toml
[[accounts]]
name = "Work"
email = "alice@work.com"

[[accounts.signatures]]
name = "Formal"
body = """
---
**Alice Smith** | Engineering
alice@work.com
"""
is_default = true

[[accounts.signatures]]
name = "Casual"
body = "— Alice"
```

## Gmail Signature Import

### New Client Method

Add to `gmail.Client` interface:

```go
GetSendAsSignature(ctx context.Context) (string, error)
```

Implementation:
1. Call `Users.Settings.SendAs.List("me")` via the Gmail API
2. Find the primary SendAs alias (where `IsPrimary == true`)
3. Extract its `Signature` field (HTML string)
4. Convert HTML to markdown using the existing `stripHTML` utility or a similar approach
5. Return the markdown string

### OAuth Scope

The `gmail.readonly` scope already covers `Users.Settings.SendAs.List` (read-only). No new scope needed.

### Import Flow

From the settings panel, pressing `i`:
1. Show "Importing..." status
2. Call `GetSendAsSignature(ctx)`
3. If successful, add/update a signature named "Gmail (imported)" with `IsDefault: false`
4. If a signature with that name already exists, update its body
5. Save config
6. Show success/error in status

## Settings Panel

### Keybinding

- Key: `,` (comma)
- Available from: inbox view only
- Must not trigger during search mode (search captures all keys already)

### UI

Modal overlay similar to the account switcher:
- Title: "Account Settings — {account name}"
- Section header: "Signatures"
- List of signatures with cursor navigation
- Default signature marked with `*` prefix
- Selected/cursor item highlighted

### Key Bindings Inside Panel

| Key | Action |
|-----|--------|
| `j/k` or `↑/↓` | Navigate signature list |
| `a` | Add new signature (prompt for name, open editor) |
| `e` | Edit selected signature body (open editor) |
| `d` | Delete selected signature (with confirmation if it's the default) |
| `i` | Import from Gmail |
| `*` | Toggle selected signature as default |
| `esc` | Close panel |

### Add/Edit Signature Flow

1. For "add": prompt for a name using a text input, then open external editor with empty content
2. For "edit": open external editor with the current signature body
3. On editor close, read back the content and save to config
4. Uses the same editor resolution as compose: config → `$EDITOR` → `vi`

## Compose Dialog — Signature Field

### Field Placement

New field in the compose dialog field order, between Subject and Attachments:

```go
const (
    composeFieldTo composeDialogField = iota
    composeFieldCC
    composeFieldBCC
    composeFieldSubject
    composeFieldSignature  // NEW
    composeFieldAttachments
)
```

### Behavior

- Shows the name of the currently selected signature
- Default: the account's default signature (or "(none)" if no signatures configured)
- Up/Down arrows cycle through available signatures + a "(none)" option
- Enter confirms selection and advances to next field
- Tab/Shift+Tab navigate past it normally
- No free-text input — purely a selector

### Display

When focused:
```
Signature:  ▸ Formal ◂     (↑/↓ to change)
```

When not focused:
```
Signature:  Formal
```

### State

New fields on the App struct:
- `composeSignatureIdx int` — index into the account's signatures slice (-1 for none)

### Signature Injection

In `launchComposeEditor()`, replace the current logic:
- Instead of calling `activeAccountSignature()` (which returns the single old-style signature), look up the signature by `composeSignatureIdx`
- If idx == -1, no signature appended
- Otherwise, append `Signatures[idx].Body` with the existing deduplication check
- Same `\n\n` separator behavior

## Files to Modify

1. **`internal/config/config.go`** — New `Signature` struct, update `Account`, add migration logic, add helper methods
2. **`internal/gmail/client.go`** — Add `GetSendAsSignature` to interface
3. **`internal/gmail/gmail.go`** (or implementation file) — Implement `GetSendAsSignature`
4. **`internal/ui/common/keys.go`** — Add `Settings` key binding (`,`)
5. **`internal/ui/common/messages.go`** — Add any new message types if needed
6. **`internal/ui/app.go`** — Settings panel state, rendering, key handling; compose dialog signature field; update `launchComposeEditor()`; update `composeFieldOrder()`
7. **`internal/ui/inbox/inbox.go`** — Handle `,` key to emit settings message
8. **`cmd/mailmd/main.go`** — Update help text

## No Breaking Changes

- Old config files with single `signature` field are auto-migrated
- If no signatures configured, compose dialog hides the Signature field (same as CC/BCC/Attachments)
- Existing reply/forward/compose flows unchanged — just uses the new picker instead of the single signature

## Testing Considerations

- Config migration: old single signature → new signatures slice
- Config migration: no signature → empty slice (no crash)
- Gmail import: HTML → markdown conversion
- Gmail import: no signature set in Gmail → graceful empty result
- Compose dialog: signature field navigation with Tab/Shift+Tab
- Compose dialog: cycling through signatures with Up/Down
- Compose dialog: "(none)" option
- Settings panel: add/edit/delete/import/set default
- Signature deduplication on re-edit still works
