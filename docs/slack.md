# Chat bridge (Slack today)

**Modular:** `internal/chat` is transport-neutral (event cursor, status filter, comment/move/list
logic, on/off). A transport is ~3 methods (`chat.Transport`: `Ready`, `Send(Msg)`, `Listen`) and is
registered in `dispatch.go`. Slack is the only one; Teams/Telegram/WhatsApp = a new package that
implements `Transport`, plus one map entry. Hot path is unchanged (poll a local table, one HTTP post),
so the abstraction costs nothing at runtime.

**User-configurable** (`conveyor config set …`, re-read every few seconds, no restart):
- `chat_enabled` `on|off` - turn the bridge off while at the desktop; events during "off" are skipped, not replayed.
- `chat_transport` - which transport (default `slack`).
- `chat_statuses` - comma list from `needs_you,review,done,failed` that send a message.

Not configurable yet: destination (always a DM to `slack_user_id`) and per-feature toggles
(replies/commands are always on) - add when a second transport or a real need appears.

## Slack

Slack is **not** an "app" in the Spotlight/Graft sense. Apps (`internal/core/apps.go`) are *outbound
launchers*: Conveyor runs a command against a worktree. Slack is a *two-way client of Conveyor*: it
reacts to task events and sends commands back. That is the shape of the Jira importer and the
dispatcher, so it lives in `internal/slack` and runs inside the dispatcher (daemon or desktop app).

Socket Mode is used so Conveyor stays local: no public URL, no tunnel.

## Your own app, not the org's (user guide)

A Slack app always lives in *a* workspace, but it is only yours if you keep it that way: create it
from your own account and never distribute it. Nobody else can see or use it, and Conveyor only
obeys your `slack_user_id`. Two choices:
- **Personal workspace** (free, create at slack.com/get-started): fully yours, no org admin needed.
- **Org workspace, private app**: create it under your account; if the org restricts app creation,
  ask an admin or use a personal workspace.

**Communication.** Conveyor opens an *outbound* WebSocket to Slack (Socket Mode) and posts via the Web
API. Nothing listens on your machine, so no public URL or tunnel. Slack on your phone talks to Slack;
Slack relays to the running Conveyor. Conveyor must be running (desktop app or daemon) to answer.

**Yes, there is an App page.** Slack shows your app's Home tab; Conveyor publishes the command
guide there once each time it connects (not on every open; Slack keeps it). Slack's website has no
static editor for Home content, only the API can set it. The **Messages** tab is on too: that is
the DM where you send commands and receive task updates. The app appears under *Apps* in your
sidebar; pin it or message it once and it shows in your DMs.

### Setup (once, ~3 min)
1. api.slack.com/apps → **Create New App → From manifest**, pick the workspace, paste:
```yaml
display_information: {name: Conveyor}
features:
  app_home: {home_tab_enabled: true, messages_tab_enabled: true, messages_tab_read_only_enabled: false}
  bot_user: {display_name: Conveyor}
  slash_commands:
    - {command: /conveyor, url: https://example.com, description: Control Conveyor tasks, usage_hint: help}
oauth_config: {scopes: {bot: [chat:write, im:history]}}
settings:
  event_subscriptions: {bot_events: [message.im]}
  socket_mode_enabled: true
```
2. **Basic Information → App-Level Tokens**: create one with `connections:write` (`xapp-…`).
   **Install App** to get the bot token (`xoxb-…`). Your member ID: profile → ⋮ → Copy member ID.
3. In Conveyor: Apps → Integrations → Slack, paste both tokens and the member ID (or
   `conveyor config set slack_bot_token|slack_user_token|slack_app_token|slack_user_id …`). No restart.

**Bot or you.** By default Conveyor posts as the bot. To act as yourself instead, add the
`slack_user_token` (`xoxp-…`, Apps → Integrations → User token): in the manifest add
`oauth_config.scopes.user: [chat:write, im:history, im:write]` and `settings.event_subscriptions.user_events: [message.im]`,
reinstall, copy the *User OAuth Token*. When set it wins over the bot token. Caveat: Slack does not
notify you for messages you post yourself, so on a phone you get no push for task updates in this mode.

Only `slack_user_id` is DMed and obeyed; everyone else is ignored.

## Using it from your phone
Open the Conveyor DM. Type a plain message, no slash needed:
- `help` - the guide (also on the app's Home tab)
- `list` - tasks waiting on you
- `show CV-12` (or just `CV-12`) - status, repos, PRs, last agent message
- `CV-12 use the v2 API` - comment on CV-12 (requeues it)
- `move CV-12 done` - todo | done | cancelled
- Reply in a notification's thread - comments on that task.

Notifications (needs_you, review, done, failed; pick which in settings) carry the agent's last
message and PR links.

## Not done (add when needed)
- Block Kit buttons/modals (needs interactive payload handling), channel mode, retry/dedupe
  beyond the event cursor (`slack_cursor`), token storage in
  Keychain (tokens sit in the local settings table like other settings).
