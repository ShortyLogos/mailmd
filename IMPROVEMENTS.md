# Improvements Plan

## High impact, likely straightforward

1. ~~**Archive**~~ (done) — Dedicated archive action (remove INBOX label, keep in All Mail). Most common Gmail action, one-line API call (`modify` to remove INBOX label). Without it users must leave messages in inbox or trash them.

2. ~~**Labels/folders beyond the big 4**~~ (done) — Browse custom Gmail labels (Starred, Important, user-created labels). Gmail's label system is a core organizing tool; supporting it makes mailmd viable as a primary client.

3. ~~**BCC field**~~ (done) — Blind Carbon Copy: recipients receive the email but their addresses are hidden from all other recipients. Easy to add since CC already works, and it's table-stakes for any email client.

4. ~~**Email signatures**~~ (done) — Configurable per-account signature (markdown block appended to compose templates). Saves users from re-typing or relying on editor snippets.

5. **Thread/conversation view** (skipped — requires new API method + major reader refactor) — Show threaded conversations (like Gmail's web UI) instead of individual messages. Makes following multi-message exchanges much easier.

## Medium impact, moderate effort

6. ~~**Starred messages**~~ (done) — Toggle star from inbox/reader, show a Starred folder tab. Gmail users rely heavily on this.

7. **Snooze** (skipped — requires background scheduler for re-surfacing) — Remove from inbox temporarily and resurface later. Gmail API supports this natively via label manipulation.

8. ~~**Contact groups / aliases**~~ (done) — Expand a group name like `team` into multiple recipients. Useful for repeat group emails.

9. ~~**Notification/new mail indicator**~~ (done) — Background polling that surfaces new mail counts or desktop notifications while working in the terminal.

## Nice to have

10. ~~**Email templates/snippets**~~ (done) — Save reusable markdown templates for common email types (standup reports, weekly updates, etc.).

11. ~~**Quoted text styling**~~ (already implemented) — Visually distinguish quoted reply text from new content in the reader.

12. ~~**Theme system**~~ (done) — The config has a `theme` field but only "default" works. A few built-in themes (dark, light, solarized) would be a nice touch.
