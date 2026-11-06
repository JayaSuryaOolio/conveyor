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

Slack is **not** an "app" in the Graft sense. Apps (`internal/core/apps.go`) are *outbound
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
static editor for Home content, only the API can set it. It is Block Kit (header, dividers, sections), not HTML. The **Messages** tab is on too: that is
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
oauth_config: {scopes: {bot: [chat:write, im:history, reactions:write, files:read]}}
settings:
  event_subscriptions: {bot_events: [message.im]}
  interactivity: {is_enabled: true}
  socket_mode_enabled: true
```
2. **Basic Information → App-Level Tokens**: create one with `connections:write` (`xapp-…`).
   **Install App** to get the bot token (`xoxb-…`). Your member ID: profile → ⋮ → Copy member ID.
3. In Conveyor: Apps → Integrations → Slack, paste both tokens and the member ID (or
   `conveyor config set slack_bot_token|slack_user_token|slack_app_token|slack_app_refresh_token|slack_client_id|slack_client_secret|slack_user_id …`). No restart.

**Bot or you.** By default Conveyor posts as the bot. If `slack_user_token` is filled it replaces the bot
token for everything Conveyor does in Slack (task DMs, thread replies, command answers, reactions): add
`oauth_config.scopes.user: [chat:write, im:history, im:write, reactions:write, files:read]` and `settings.event_subscriptions.user_events: [message.im]`,
reinstall and copy the *User OAuth Token*. Clear the field to go back to the bot. Caveat: messages then land in
your own self-DM and Slack does not notify you for messages you post yourself, so you get no push.

**Rotating app token.** If Slack gave you an `xoxe.xapp-…` token (12h expiry) and an `xoxe-1-…` refresh token,
put them in `slack_app_token` and `slack_app_refresh_token`, and the app's Client ID and Client Secret
(Basic Information → App Credentials) in `slack_client_id` and `slack_client_secret`. When the token expires
Conveyor calls `oauth.v2.access` (`grant_type=refresh_token`) and saves the new pair itself.
(`tooling.tokens.rotate` only rotates app *configuration* tokens and answers `invalid_refresh_token` here.)

Only `slack_user_id` is DMed and obeyed; everyone else is ignored.

## Using it from your phone
Open the Conveyor DM. Type a plain message, no slash needed:
- `help` - the guide (also on the app's Home tab)
- `list` - tasks waiting on you
- `show CV-12` (or just `CV-12`) - status, repos, PRs, last agent message
- `CV-12 use the v2 API` - comment on CV-12 (requeues it)
- `move CV-12 done` - todo | done | cancelled
- `new [REPO] your comment` - new backlog task. Forward a Slack message (images too) to the
  Conveyor DM, then send `new …` or `/conveyor new …`; or type the comment in the forward box.
  The description is your comment, the forwarded text with author and link, and its images
  (saved under `~/.conveyor/attachments`, needs `files:read`). REPO can be left out with one
  registered repo. Slash commands only work in the app's DM; `@app` mentions in channels are not wired.
- Reply in a notification's thread, or to the `created KEY` answer to `new` - comments on that task.
  `move done`, `move to done`, `show`, `list` or `help` typed there run as commands on that task instead.
- DM commands are answered in their own thread; a task made by `new` gets its notifications in that thread.
  Slack redeliveries are ignored, and "also send to conversation" thread replies count as replies.

Notifications (needs_you, review, done, failed; pick which in settings) carry the agent's last
message (rendered as Markdown via a Block Kit `markdown` block) and PR links, plus buttons: **Done** / **Requeue** (while it needs you, is in review or failed)
and **Open PR**. Buttons need *Interactivity* on: in the manifest add `settings.interactivity: {is_enabled: true}`
(Socket Mode needs no request URL). Each task gets one
thread: later notifications for it, and the agent's answers to your thread replies, post into it.
Thread links are stored in settings (`slack_thread:CHANNEL:TS` -> task key, `slack_task:KEY` -> `CHANNEL:TS`), so
routing does not parse message text and survives restarts; only threads from before the links existed fall back to
reading the thread (`conversations.replies`). Rows are never pruned.

### Slack behaviours the bridge relies on (docs.slack.dev)
- A message is identified by channel + `ts`; `thread_ts == ts` marks a thread root, replies carry the root's `thread_ts`.
- Events not acked within 3s are redelivered (up to 3 times, ~1 and ~5 min later). The envelope is acked first, handling runs
  on one ordered worker, and handled `channel/ts` are remembered in memory so a redelivery is ignored.
- Our own posts come back as events (user token) and edits/deletes arrive as `message_changed` / `message_deleted`
  subtypes; only plain, `file_share` and `thread_broadcast` messages are handled.

## Not done (add when needed)
- Block Kit modals, channel mode, replying to an ephemeral `/conveyor` answer (Slack cannot thread them), token storage in
  Keychain (tokens sit in the local settings table like other settings).
