---
"conflux": minor
---

Add Shoutrrr-backed notifications for reconcile runs that deploy, remove, or fail stacks.

Conflux can now send notifications to services like Telegram and Discord using the `CONFLUX_NOTIFY_URLS` environment variable, with multiline summaries that include the affected stack names.
