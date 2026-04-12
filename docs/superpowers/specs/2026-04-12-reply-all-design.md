# Reply-All Feature Design

**Date:** 2026-04-12  
**Scope:** Add keyboard-driven reply-all functionality to the email reader

## Overview

Currently, pressing 'r' initiates a reply to only the original sender. This spec adds a reply-all feature (Shift+R) that includes all original message recipients (To, CC fields) in the composed reply, while excluding the current user to avoid self-replies.

## Keyboard Binding

- **Key:** Shift+R (capital R)
- **Help text:** "reply all"
- **Scope:** Available in reader view (same contexts as regular reply)

## Recipient Handling Logic

When user presses Shift+R on a message:

1. **Gather original recipients:**
   - Extract `To` field from original message
   - Extract `CC` field from original message
   - Extract `From` field (original sender)

2. **Deduplicate and filter:**
   - Remove current user's email from all fields (avoid self-reply)
   - Remove duplicate addresses across To/CC/From
   - Preserve field structure: original To stays in To, original CC stays in CC

3. **Populate compose dialog:**
   - **To:** Original message's `To` recipients (minus current user)
   - **CC:** Original message's `CC` recipients (minus current user)
   - **Special case:** If original sender is not already in To/CC, add to To field
   - **Subject:** "Re: " + original subject (same as regular reply)
   - **Body:** Quoted message (using existing `quoteBody()` function, identical to reply)
   - **ThreadID/InReplyTo:** Preserve thread context (same as regular reply)

4. **Edge case — no valid recipients:**
   - If deduplication leaves To empty but From exists, put From in To
   - This ensures at least the original sender is always a recipient

## Implementation Changes

### 1. `internal/ui/common/keys.go`
- Add `ReplyAll` field to `KeyMap` struct
- Initialize with: `key.NewBinding(key.WithKeys("shift+r"), key.WithHelp("shift+r", "reply all"))`

### 2. `internal/ui/reader/reader.go`

**Add helper function:**
```go
// gatherReplyAllRecipients builds To/CC lists for reply-all,
// excluding the current user's email address.
func gatherReplyAllRecipients(msg *gmail.Message, currentEmail string) (to, cc []string)
```

Logic:
- Collect To field into `to` slice
- Collect CC field into `cc` slice  
- Add From to `to` if not already present
- Remove all instances of `currentEmail` from both slices
- Return deduplicated lists

**Add case handler in Update():**
- Match against `key.Matches(msg, common.Keys.ReplyAll)`
- Call `gatherReplyAllRecipients()` to build To/CC
- Emit `ComposeMsg` with populated recipients, "Re: " subject, quoted body, thread context

## No Breaking Changes

- Regular reply (r) behavior unchanged
- No UI modifications needed
- Help text automatically includes new binding
- Compose dialog already supports To/CC/BCC arrays

## Testing Considerations

- Single recipient → reply-all includes them
- Multiple To recipients → all included
- CC recipients → preserved in CC
- User in original recipients → filtered out
- User as only original recipient → gracefully handles
- BCC recipients → not included (per email RFC; BCC hidden from recipients)

## Files Modified

1. `internal/ui/common/keys.go`
2. `internal/ui/reader/reader.go`
