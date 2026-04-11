---
title: Contact Cache
last-updated: 2026-04-10
areas: [internal/contacts/contacts.go]
---

# Contact Cache

Per-account JSON file storing email contacts for compose autocomplete.

## How It Works

- Contacts are persisted at `~/.config/mailmd/contacts/{email}.json` as a flat JSON array
- Each contact stores email, optional display name, and last-used timestamp
- On send or draft save, `App` calls `contacts.Add()` in a goroutine with all To/CC addresses
- `Add()` merges: existing contacts get `LastUsed` updated; new addresses are appended
- `All()` returns contacts sorted most-recently-used first, formatted as `Name <email>` or bare email
- Addresses are parsed via `net/mail.ParseAddress` (RFC 5322) with fallback to bare `@`-containing strings

### Key Files

- `internal/contacts/contacts.go` — `Load`, `Save`, `Add`, `All`, `ParseAddresses`
- `internal/contacts/contacts_test.go` — unit tests for add/merge/parse
- `internal/ui/app.go` — `contactsPath()`, contact loading on dialog open, `contacts.Add()` on send/save
