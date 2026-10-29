# Slack bridge

Slack is **not** an "app" in the Spotlight/Graft sense. Apps (`internal/core/apps.go`) are *outbound
launchers*: Conveyor runs a command against a worktree. Slack is a *two-way client of Conveyor*: it
reacts to task events and sends commands back. That is the shape of the Jira importer and the
dispatcher, so it lives in `internal/slack` and runs inside the dispatcher (daemon or desktop app).

Socket Mode is used so Conveyor stays local: no public URL, no tunnel.

## Setup (once)
1. api.slack.com/apps → new app. Enable **Socket Mode**; create an app token with `connections:write`.
2. Bot scopes: `chat:write`, `im:history`. Event subscription: `message.im`. App Home → allow messages.
3. Slash command `/conveyor` (any URL; Socket Mode ignores it). Install to your workspace.
4. `conveyor config set slack_bot_token xoxb-…`, `slack_app_token xapp-…`, `slack_user_id U…`
   (your member id). Settings are re-read every few seconds; no restart.

Only `slack_user_id` is DMed and obeyed; everyone else is ignored.

## What works
- Task moves (by the system) to `needs_you`/`review`/`done`/`failed` → DM with the agent's last
  message and PR links. Replies in that thread become a comment (which requeues the task).
- `/conveyor` lists tasks needing you · `/conveyor KEY text` comments · `/conveyor move KEY todo|done|cancelled`.

## Not done (add when needed)
- Block Kit buttons/modals (needs interactive payload handling), channel mode, retry/dedupe
  beyond the event cursor (`slack_cursor`), a Settings UI field for the tokens, token storage in
  Keychain (tokens sit in the local settings table like other settings).
