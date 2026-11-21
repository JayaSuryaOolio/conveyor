Request: let me create a webhook on <OWNER/REPO> (instant PR review notifications)

Hi,

I use a local tool (Conveyor) that watches the pull requests I open and reacts when a review or comment arrives. It uses `gh webhook forward` (the official GitHub CLI extension, github.com/cli/gh-webhook). It creates a repo webhook that GitHub relays to my laptop over an outbound websocket. Nothing is exposed publicly and no tunnel is involved.

Creating a repo webhook needs admin-level access, which I don't have on <OWNER/REPO>. Could you do ONE of these?

Option A (preferred, least privilege): give me a custom repository role (base role Write) that includes the "Manage webhooks" permission. Custom roles are an Enterprise Cloud feature; if the org doesn't have them, use Option B.

Option B: grant me the Admin role on <OWNER/REPO> (it can be temporary).

What the tool does with the access:
- Runs `gh webhook forward --repo <OWNER/REPO> --events=pull_request_review,pull_request_review_comment,issue_comment`
- It adds one webhook to the repo (GitHub allows only one such forward per repo) and subscribes only to those three events: reviews, review comments, and PR conversation comments.
- It changes no code, settings or branches, and it only reads event payloads.
- If a webhook is ever left behind it can be deleted under Settings > Webhooks.

My GitHub token also needs the `admin:repo_hook` scope; I will add that on my side (`gh auth refresh -s admin:repo_hook`).

If you would rather not grant this, that is fine: the tool falls back to polling and I just get review notifications later (30 min, 2 h, 8 h backoff).

Thanks!
