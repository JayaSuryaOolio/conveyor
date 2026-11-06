# Epic Builder: PRD and tech spec

Status: draft v4 (CV-4).
- v1 put an `epic_stage` column on tasks. Rejected.
- v2 used labels and relations only, and planned with wayfinder labels.
- v3 applies the answers to v2's questions: a default plan gate that `epic_gate:off` turns off, no auto-merge, an internal **epic flow** (one attempt at the epic) that holds PR state, React Flow with editable cards, and `epic:*` labels as an adapter over any planning skill. It also adds the `epic` skill and its checklist, event-driven review checks instead of polling, and audit logs you can query as OTel.
- v4 applies the answers to v3's questions: the epic label is `epic:<name>` with any name (`epic:os-4`), and kinds move to `epic_kind:<kind>` (§2.1). Review webhooks ship as an opt-in **app** that Conveyor offers to add as a button (§10). Audit is plain JSONL, no OTel (§12). Restart is a button; the human decides, nothing proposes it (§2.3).

Decisions are in §3. There are no open questions.

---

# Part 1: PRD

## 1. Problem

Conveyor runs one task at a time well. A piece of work the size of a Jira epic (several PRs, maybe several repos) still needs a human to:

- work out the requirements and the acceptance criteria (AC),
- split the work into tasks and put them in order,
- pick the right agent and skills for each task,
- watch each task, copy PR review comments back to it, and check that the pieces add up.

That coordination takes the time. It is also hard to see where an epic stands, because the board is a flat list of cards.

## 2. What we're building

1. **An autonomous epic loop.** You describe a large task once. Conveyor plans it and asks you to approve the plan. After that it runs, fixes review comments, and waits for you to merge.
2. **The Epic view.** A flow chart of the task tree with live status. **On each card you can see and change the agent, model and skills** for that step, and start, stop or answer it.
3. **The `epic` skill.** It is installed with Conveyor's hooks and keeps every epic run on a checklist (§2.4).
4. **Audit logs.** Every epic action is one JSON line in `audit.jsonl`, which you can query with `conveyor epic audit`, `jq`, or ship anywhere that reads JSON lines.

The building blocks:

| Concept | How it's expressed | New? |
|---|---|---|
| "This task is an epic" | label `epic` on the root task | no (labels, #131) |
| Membership | label `epic:<name>` on the root and every task in the epic, plus `parent_id`. The name is any string you pick (`epic:os-4`, `epic:payments-v2`) and is unique among epics that are not done | label convention |
| Stage | label `epic_stage:<stage>` on the root, one at a time | label convention |
| Kind of child | label `epic_kind:<kind>`: `research`, `prototype`, `grilling` or `build` (§2.1) | label convention |
| Repo | label `repo:<name>`, kept equal to the task's repos | label convention |
| Skills for a step | labels `skill:<name>`; the brief tells the agent to load them | label convention |
| Agent and model for a step | `tasks.agent`, `tasks.model` | no |
| Order | `blocks` links. The dispatcher already holds blocked tasks back. | no |
| Hierarchy | `parent_id` | no |
| **Epic flow** | one attempt to finish the epic's tasks. It holds PR state, review checks and the audit trail. Hidden in the UI; you can query it. | **yes**, table `epic_flows` |

The board stays the source of truth for tasks. The flow chart is relations drawn as a picture, and it can also edit them.

### 2.1 Labels as an adapter over planning skills

Every epic label has one meaning per prefix, so a filter never mixes them:

| Prefix | Means | Example |
|---|---|---|
| `epic` (bare) | this task is an epic root | `epic` |
| `epic:` | belongs to the epic with this name | `epic:os-4` |
| `epic_stage:` | the root's current stage | `epic_stage:build` |
| `epic_kind:` | what sort of step a child is | `epic_kind:grilling` |
| `epic_planner:`, `epic_gate:` | root settings | `epic_planner:wayfinder`, `epic_gate:off` |

**What a kind is.** A kind says what sort of work a child task is, so Conveyor and the agent know how to run it and when it counts as finished. There are four:

| Kind | Purpose | Human? | Output | Stage it belongs to |
|---|---|---|---|---|
| `grilling` | settle a question by asking you | yes: the run asks and waits in `needs_you` | a decision line on the root's description | `decide` |
| `prototype` | try something to answer a design question | yes: you look at it and react | a throwaway branch or notes, never a PR to merge | `decide` |
| `research` | read code or docs and report | no | findings in the status block and on the root | `decide` |
| `build` | ship part of the epic | no (except review and merge) | a PR, test-first, citing AC ids | `build`, and new ones from `verify` |

How the kind is used:
- **Brief.** The brief adds one line per kind that tells the agent what to do and what not to do (for example, `grilling`: "ask, record the decision, open no PR"; `build`: "write the failing test first, open one PR, cite AC ids").
- **Stage moves.** The epic agent moves from `decide` to `spec` when no open `grilling`, `prototype` or `research` child is left, and from `build` to `verify` when no open `build` child is left.
- **Checks.** `epic check` requires a repo, an agent and AC ids only on `build` children.
- **Epic view.** The card shows the kind as a coloured chip, so you can see at a glance which steps will want you.

Kind is a label, so you can change it on the card. Go reads it only in the brief and in `epic check`; the rest is in the skill.

Conveyor speaks this one vocabulary. A **planner adapter** maps a planning skill's operations onto it. The default is Matt Pocock's [`wayfinder`](https://github.com/mattpocock/skills/blob/main/skills/engineering/wayfinder/SKILL.md):

| wayfinder | Conveyor |
|---|---|
| map issue | the epic root. Its description holds Destination, AC, Decisions so far, Not yet specified, Out of scope. |
| ticket `wayfinder:research\|prototype\|grilling\|task` | sub-task labelled `epic_kind:research\|prototype\|grilling\|build` |
| native blocking | `conveyor task link A blocks B` |
| frontier | children in `todo` with no open blockers. The dispatcher launches exactly these. |
| claim | the dispatcher starting a run |
| HITL ticket (grilling, prototype) | the run asks its questions and goes to `needs_you`; your comment resumes it |
| AFK ticket (research, build) | runs with no human |
| resolution, close, pointer on the map | status block summary, task `done`, one line appended to the map's Decisions so far |

An adapter is one markdown file, `skills/epic/planners/<name>.md`, holding this table as commands. The root picks a planner with `epic_planner:<name>` (default `wayfinder`). Another skill, such as a spec-kit style planner or your own grilling skill, needs only a new file. No Go changes.

### 2.2 The loop

```
 chart ──► [approve] ──► decide ──► spec ──► build ──► verify ──► done
   │                       │          │        │          │
 epic run               decision    epic run  build      epic run
 (grills you,           tickets     (spec +   tickets    (AC vs merged
  writes map + AC)      run AFK     build     (TDD, PR,  work; gaps →
                        or HITL)    tickets)  review     new build
                                              fixes)     tickets)
```

| Stage | What happens | Moves on when | Human? |
|---|---|---|---|
| `chart` | The epic run grills you on the destination and the AC, then creates the decision tickets and their `blocks` links. | Map and AC written | Yes. This is the main conversation. |
| *gate* | The run ends in `needs_you` with the plan. **Approve** (button on the Epic view, or a comment `approve`) moves the epic to `decide`, and everything after runs without you. With label `epic_gate:off` there is no stop. | Approved | Yes, unless `epic_gate:off` |
| `decide` | Frontier tickets run as they unblock. A resolution can add tickets. | No open decision tickets | Only on grilling and prototype tickets |
| `spec` | The epic run writes the spec, creates `epic_kind:build` sub-tasks (repo, AC ids, agent, skills) with `blocks` links, and moves them to `todo`. | Build tickets exist | No |
| `build` | Build tickets run test-first and open PRs. Review events requeue them (§10). | Every build child done or cancelled | When an agent gets stuck, and **for merging: the human always merges** |
| `verify` | The epic run checks every AC against the merged work. Gaps become new build tickets and the stage goes back to `build`. | All AC met | No |
| `done` | The epic moves to done, and the flow ends. | | |

The epic agent swaps its own stage label, so moving between stages needs no Go code. Go adds one rule: when the last open child settles, wake the epic (§9).

### 2.3 Epic flows (attempts)

An **epic flow** is one attempt to finish the epic. It is not a task and is not shown on the board.

- Approving the plan (or the first run with `epic_gate:off`) starts flow 1.
- The flow records the tasks it runs, their PRs (head commit, review decision, checks, unresolved count), the one-shot review checks it scheduled (§10), and every audit record (§12).
- **You decide when to restart.** The Epic view header has a **Restart** button (it asks for a reason). It, or `conveyor epic restart KEY --reason "…"`, ends the flow as `abandoned`, cancels its open tasks, and starts flow N+1 at `spec` with the map and decisions kept. Nothing counts failures, proposes a restart or restarts on its own.
- You can query flows (`conveyor epic flows KEY`, `conveyor epic status KEY --flow N`); the UI shows only the current one.

### 2.4 The `epic` skill and its checklist

The skill ships in this repo at `skills/epic/`. "Install hooks" (Settings, `UserHooks("install")`) copies it to `~/.claude/skills/epic/`, and "uninstall" removes it. Every epic run and every child run loads it (the brief names it).

Tasks remember nothing between runs. The skill makes them seem to: **every run starts from `conveyor epic status KEY --json`**, which assembles flow, tasks, PRs, AC coverage and the recent audit tail from the DB. That status is the state. The workspace's STATE.md is only a scratchpad.

Checklist, run at the start and before the status block (`conveyor epic check KEY` does the mechanical items and prints what fails):

1. **Epic label.** Every task in the tree has the root's `epic:<name>`, and no task outside the tree has it.
2. **Repo labels.** Each task's `repo:*` labels equal its repos. A build task has exactly one repo.
3. **Acceptance criteria.** The root's `## Acceptance criteria` is a numbered checklist (AC1, AC2, …). Every AC is covered by at least one build task that cites its id, and each AC is ticked only with evidence (PR, test, or commit).
4. **PRs.** Every build task in `review` or `done` has a PR recorded on the flow. `conveyor epic prs KEY [--commits]` lists PRs and their commits. `conveyor epic search KEY <regex>` runs `git log -G` over the flow's PR branches to find where something changed.
5. **Sessions.** `conveyor epic sessions KEY [--grep <text>]` lists each run's Claude session (task, run, transcript path) and searches the transcripts. Use it to recall why something was done instead of asking the human again.
6. **Order and agents.** No `epic_kind:build` task in `todo` lacks an agent, a repo or AC ids. No `blocks` cycles (already refused).
7. **Audit.** Every stage change, task created, label changed, gate passed and restart is in the audit log. Hooks write most records (§12); the skill only adds the reason with `conveyor epic log KEY "<why>"`.

## 3. Decisions (v3 questions)

1. **Label namespace.** `epic:<name>` is membership only, and the name is any string (`epic:os-4`). Kinds are `epic_kind:<kind>`. See §2.1.
2. **Review webhooks** are opt-in. `gh webhook forward` ships as a Conveyor **app** (CV-55 manifest). When an epic has open PRs and the app is not installed, the Epic view offers to add it as a button (§10).
3. **Audit** is `audit.jsonl` and nothing else: no OTel field names, no collector setup, no Claude OTel env. It is written with Go's `log/slog` JSON handler, so it can become the pattern if more of Conveyor's logging moves to structured logs later.
4. **Restart.** No threshold. The human decides with a visible Restart button.

## 4. Requirements

**Epic loop**
- R1. Adding `epic` and `epic:<name>` to a root task makes it an epic; with no `epic_stage:*` label, the stage is `chart`. "New task" gets an "Epic" toggle with a name field (default: the task key, lower-cased). Two epics that are not done cannot share a name.
- R2. `conveyor task label KEY set <prefix>:<value>` replaces labels with the same prefix in one transaction.
- R3. After `chart`, the epic waits in `needs_you` for approval unless it has `epic_gate:off`. Approval starts a flow and moves the epic to `decide`. Nothing after that needs the human, except HITL tickets, stuck agents, and merging.
- R4. No agent merges a PR. The `epic` skill says so, and the PreToolUse hook denies `gh pr merge` in runs of epic tasks.
- R5. Children carry `epic:<name>`, `epic_kind:<kind>` and `repo:*`. Build tickets carry their AC ids and a link to the spec, and are told to work test-first.
- R6. While any child is running or in `todo`, the epic does not run. When the last open child settles, the epic is requeued with a system comment that names its stage.

**Flows**
- R7. `epic_flows` records attempts. Only one flow per epic is active. `conveyor epic flows|status|restart` work on flows. Only the human restarts a flow. The flow id never appears on the board or the cards.

**Skill**
- R8. `skills/epic/` is installed and removed together with the user hooks. It holds the checklist (§2.4), the stage loop and `planners/wayfinder.md`.
- R9. `conveyor epic status|check|prs|search|sessions|log` exist and print `--json`.

**Review checks**
- R10. Review comments reach the task through **one-shot automations** owned by the task and the flow, not a recurring poll (§10). They are deleted when the task is done or cancelled, or the flow ends. An open epic starts the review-webhook listener itself; the review-webhooks app opts non-epic tasks in.

**Audit**
- R11. Every epic action produces one JSON line in `audit.jsonl` (§12). `conveyor epic audit KEY [--flow N] [--since]` queries it.

**Epic view**
- R12. A page per tree, opened from any task (resolves to the root). Built on React Flow. Children are grouped under their parent; columns follow `blocks`.
- R13. **Each card shows and edits** the agent (picker of the user's and repos' agents), model, skills (multi-select of the skills found on disk, stored as `skill:*` labels; the agent's own frontmatter skills are shown read-only), repo, and labels. It also shows the key, title, status with a pulse while running, a worktree chip and a PR chip (state, review, checks, unresolved count). Card actions: open task, run or requeue, stop the run (CV-56), answer (when `needs_you`), open the PR, open a shell or editor.
- R14. On the canvas: drag from one card to another to add a `blocks` link, select an edge and delete it to unlink, and use "+" on a group to add a sub-task. Every edit calls the same core functions as the CLI.
- R15. A header strip shows the stage pipeline with the current stage, `n/m` children done, the "wants you" count, at the gate **Approve plan**, and always **Restart** (asks for a reason). If the epic has PRs and the review-webhooks app is not installed, it shows **Add review webhooks** once (dismissable).
- R16. Updates are live on `db:changed`.
- R17. Board cards and the task page header get a flow icon that opens this view. A lone task shows as one node plus its link neighbours.

## 5. Out of scope (v1)

- Auto-merge, in any form.
- Saved node positions. Custom stage lists per epic.
- Jira import or sync.
- Worktree detail (dirty, ahead/behind).
- Editing an agent's own file from the card (the card links to the Agents page instead).

## 6. Success

- A one-paragraph epic reaches PRs ready to merge, with the human only answering grilling questions, approving the plan and merging.
- From the Epic view you can tell in five seconds the stage, what is blocked, what wants you, which PR has unread review comments, and which agent and skills each step uses, and change them on the spot.
- For any epic, `conveyor epic audit` or a Grafana query answers "who did what, when and why".

---

# Part 2: Tech spec

## 7. Data model

```sql
CREATE TABLE IF NOT EXISTS epic_flows (
  id INTEGER PRIMARY KEY,
  epic_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  attempt INTEGER NOT NULL,                 -- 1, 2, …
  status TEXT NOT NULL,                     -- active | abandoned | done
  reason TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL, ended_at TEXT,
  UNIQUE (epic_id, attempt)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_epic_flows_active ON epic_flows(epic_id) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS epic_flow_prs (  -- also the flow's task membership
  flow_id INTEGER NOT NULL REFERENCES epic_flows(id) ON DELETE CASCADE,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  repo_id INTEGER REFERENCES repos(id),     -- NULL until the task has a PR
  pr_url TEXT, head_sha TEXT, review TEXT, checks TEXT,
  unresolved INTEGER NOT NULL DEFAULT 0,
  seen_comment_id INTEGER NOT NULL DEFAULT 0, -- review watermark
  updated_at TEXT NOT NULL,
  UNIQUE (flow_id, task_id, repo_id)
);

ALTER TABLE automations ADD COLUMN run_at TEXT;       -- one-shot: fire once at run_at, then delete
ALTER TABLE automations ADD COLUMN task_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE;
ALTER TABLE automations ADD COLUMN flow_id INTEGER REFERENCES epic_flows(id) ON DELETE CASCADE;
```

- One table holds both the flow's tasks and their PRs, so restart and the PR chips need no second table.
- The `ALTER`s go through the existing idempotent `migrateAddX` pattern. An automation with `run_at` set ignores `schedule`.
- Commits are not stored. `epic prs --commits` and `epic search` read them from git and `gh` on demand.
- `store.SetPrefixedLabel(db, taskID, "epic_stage:build")` deletes the task's labels with the same prefix and adds the new one in one transaction.

## 8. Epic agent, skill and brief

- **Agent** `epic-builder`, seeded into `~/.claude/agents` by `hooks install` (no UI). The prompt is short: "You run an epic. Load the `epic` skill and follow it." Stage logic lives in the skill, so it can change without a Go release.
- **Skill files**: `skills/epic/SKILL.md` (loop, gate, checklist, no-merge rule, commands), `skills/epic/planners/wayfinder.md` (§2.1 table as commands). They are embedded with `go:embed` and written by `UserHooks("install")` next to the hooks. `status` reports the skill too.
- **Brief** (`internal/brief`):
  - a `Labels:` line,
  - for epic members: `Epic <name> (root <ROOTKEY>), stage <s>, flow <n>. Load the epic skill and run conveyor epic status <ROOTKEY> first.`,
  - for `epic_kind:*`: the one-line rule for that kind (§2.1),
  - for `skill:*` labels: `Load these skills before starting: a, b.`,
  - update the golden test.
- **Model and agent** per task already flow into the launch (`tasks.agent`, `tasks.model`). Nothing changes there.

## 9. Waking the epic

Nothing wakes a parent today when its children finish. Add one query to the tick: root tasks labelled `epic` that are not done, cancelled or running, have an active flow, have no children in `todo`, `running` or `needs_you`, and have a child that finished after the epic's last run ended. Requeue each with a system comment, `all sub-tasks settled (stage <s>)`, and write an audit record.

There is no restart logic in the dispatcher. A failed child goes to `needs_you` as today; restart is the human's button (§2.3).

## 10. Review checks: events, not polling

There is no recurring review poll. A review check is a **one-shot automation** (`run_at`, `task_id`, `flow_id`) that runs `conveyor epic review-check <TASK>` once, then deletes itself.

**What schedules a check:**

| Event | Source | Check at |
|---|---|---|
| PR opened, or the agent pushes to the PR branch | PreToolUse/Stop hook sees `gh pr create` or `git push`; the existing PR refresh notices a new head | now + 10 min (replaces any pending check for that task) |
| A check found nothing new | the check itself | next step of the backoff: 30 min, 2 h, 8 h, then stop |
| Review submitted, or review comment created | the opt-in review-webhooks app (below) | now |
| You click "check reviews" on the card | UI | now |

At most one pending check exists per task. Scheduling replaces the old one by `(task_id, kind)`. After the backoff runs out, the PR goes quiet until the next push, webhook or click. This handles "human reviews two days later" without polling all the time.

**Review-webhooks app (opt-in).** GitHub can't reach a desktop app without a tunnel, so this uses the `gh webhook` extension. It ships as an app manifest in `examples/review-webhooks.json`, using what CV-55 already has, so it needs no new UI:

```json
{"name":"Review webhooks",
 "actions":[{"id":"forward","label":"Review webhooks","places":["global_nav"],
   "run":"conveyor review-webhook",
   "service":{"status":"conveyor review-webhook --status","stop":"conveyor review-webhook --stop"}}]}
```

- `conveyor review-webhook` listens on `127.0.0.1` (random port, pid file in `~/.conveyor/`) and starts one `gh webhook forward --repo <r> --events=pull_request_review,pull_request_review_comment --url <listener>` per repo that has an open epic PR. An event schedules that task's check for now. It exits with a clear message if the `gh webhook` extension is missing.
- One forward per repo is a GitHub limit; a second Conveyor on the same repo just fails that repo's forward, and the backoff checks still run.
- **Prompt to add it:** the Epic view shows **Add review webhooks** when the epic has a PR and the app is not installed. Clicking installs the bundled manifest (same path as importing an app), and the service button appears in the nav. Without it, everything works on push-triggered checks and backoff.

**Cleanup:** when a task goes to `done` or `cancelled`, the store's status change deletes its automations. When the flow ends, `ON DELETE CASCADE` and an explicit delete do the same. Nothing lives in memory, so there is nothing to leak.

**The check** (`internal/gh`): `gh api .../pulls/{n}/reviews`, `.../pulls/{n}/comments` and a GraphQL `reviewThreads(isResolved)` call, under a 30 s context. It keeps comments newer than `seen_comment_id`, not starting with `ai agent:`, and not on resolved threads. If any are left, it posts one system comment (`PR review on <url>:` then `> file:line` and the body for each) and requeues through the existing comment path, so the debounce applies. It updates `review`, `checks`, `unresolved` and the watermark on `epic_flow_prs`. The existing 60 s `pollPRs` (PR open/merged state) stays as it is.

**Tests:** a fake `gh` returning reviews. One check posts one comment; a second posts nothing. The backoff schedules the next step. A done task leaves no automations. The dispatcher fires a `run_at` automation once.

## 11. Epic view (frontend)

**Library:** `@xyflow/react`. Custom node type `TaskNode` for the card and `group` nodes for parents. Layering is ours, about 40 lines, no dagre. Use React Flow's `nodrag` class on card controls so that pickers and buttons work inside a draggable node. That is the known pitfall with interactive nodes.

**Binding** (`internal/ui/app.go`, thin over `core`):

```go
type EpicGraph struct {
    Root  string       `json:"root"`
    Stage string       `json:"stage"`
    Gate  bool         `json:"gate"`          // waiting for plan approval
    Nodes []GraphCard  `json:"nodes"`         // TaskCard + Agent, Model, Skills, AgentSkills, Running, Worktree, PRs
    Links []store.Link `json:"links"`
}
func (a *App) EpicGraph(key string) (EpicGraph, error)
func (a *App) EpicApprove(key string) error
func (a *App) EpicRestart(key, reason string) error
func (a *App) SetTaskSkills(key string, skills []string) error // rewrites skill:* labels
```

The agent and model pickers reuse `ListAgents`, `SetTaskAgent` and the existing model setter. The skills list reuses `core/completions.go`'s `scanCommands` (repos first, then the user's). Link edits reuse `task link` and `unlink`. The `blocks` loop check already exists.

**Layout:** group children per parent. Within a group, the column is the longest `blocks` path from a child with no blockers in the group, and the row is ordered by key. Cross-group `blocks` edges are drawn but don't affect columns. `blocks` edges are solid with an arrow; `relates` edges are dashed.

**Navigation:** `App.tsx` view `{name:'epic', key}`, shortcut `e`, and an `IconSitemap` on board cards (`components.tsx`) and in the task header (`views.tsx`). "Epics" in the sidebar is the board filtered by `label:epic`.

## 12. Audit logs

**Writers.** Agents are not trusted to log. Most records come from code that already runs on every epic action:
- the hooks (`UserPromptSubmit`, `PreToolUse`, `Stop`) for runs of epic member tasks: run started and ended, `conveyor`, `gh` and `git push` commands, a denied merge, the status block,
- the store when it changes the stage label, creates a flow, ends a flow or deletes an automation,
- the dispatcher when it wakes the epic, a review check fires or a restart is proposed,
- `conveyor epic log KEY "<why>"` for the agent's reasons.

**Format.** One JSON line per record in `~/.conveyor/logs/audit.jsonl`, written by `log/slog`'s `JSONHandler` (stdlib, no new dependency):

```json
{"time":"2026-11-01T10:02:03Z","level":"INFO","msg":"stage build -> verify",
 "event":"epic.stage","epic":"os-4","root":"CV-4","flow":2,"task":"CV-4C",
 "run":812,"session":"…","actor":"agent|human|conveyor"}
```

`conveyor epic audit` reads this file and filters it; `jq` works too. Nothing is copied into the `events` table. Log rotation uses size-based rename, like the existing logs. If more of Conveyor moves to structured logging later, it can reuse this handler.

## 13. CLI

```
conveyor task label KEY set <prefix>:<value>
conveyor epic status KEY [--flow N] [--json]     # the "state" every run starts from
conveyor epic check KEY [--fix]                  # checklist §2.4; --fix repairs labels only
conveyor epic prs KEY [--commits] [--json]
conveyor epic search KEY <regex>                 # git log -G over the flow's PR branches
conveyor epic sessions KEY [--grep <text>]
conveyor epic flows KEY
conveyor epic restart KEY --reason "…"
conveyor epic approve KEY
conveyor epic log KEY "<message>"
conveyor epic audit KEY [--flow N] [--since 24h] [--json]
conveyor epic review-check TASK                  # what the one-shot automation runs
conveyor review-webhook [--status|--stop]        # what the opt-in app's service button runs
conveyor task graph KEY [--json]
```

## 14. Delivery plan (sub-tasks)

| # | Sub-task | Depends on |
|---|---|---|
| A | One-shot automations (`run_at`, `task_id`, `flow_id`, cleanup on done/cancel) and review checks with backoff (§10). Useful for any task. | — |
| A2 | Review-webhooks app: `conveyor review-webhook`, bundled manifest, "Add review webhooks" prompt (§10) | A |
| B | `epic_flows`, `epic_flow_prs`, `SetPrefixedLabel`, `task label set`, unique epic name, epic wake-up, gate and approve, restart, no-merge hook rule, `epic status/check/flows/restart/approve/prs/search/sessions` (§7, §9, §13) | — |
| C | Audit log: `slog` JSON writer, records from hooks, store and dispatcher, `epic log/audit` (§12) | B |
| D | `epic` skill + wayfinder planner adapter, installed with the hooks; agent template; brief lines (epic, kind, skills) + golden test (§2.1, §2.4, §8) | B |
| E | Epic view on React Flow: editable cards (agent/model/skills/kind/repo/labels), edge editing, stage strip with Approve and Restart, icons (§11) | B (A for the review chips) |
| F | Dogfood one real epic end to end, then fold findings into the skill and this doc | A–E |

A, A2 and C are general features. Each gets its own PR, and neither is mixed into the Epic view PR.

## 15. Testing

- Store: `SetPrefixedLabel` replaces only the same prefix (`epic:` does not touch `epic_kind:`); one active flow per epic; two open epics can't share a name; a done task deletes its automations.
- Dispatcher: the epic wakes when its children settle and not before; a `run_at` automation fires once; no restart happens without the human.
- review-webhook: a posted review event schedules a check for now; missing `gh webhook` extension gives a clear error.
- gh: review fetch, watermark, `ai agent:` skip, resolved-thread skip, backoff schedule.
- Hook: `gh pr merge` denied in epic member runs; audit records written for run start and end.
- CLI: `epic check` flags a missing `epic:<name>` label, a `build` child without AC ids, a repo label mismatch, and an AC without a covering task.
- Brief: golden test for an epic member with `epic_kind:build` and `skill:*` labels.
- UI: `npm run build`; a manual check in `wails dev` with a tree seeded through the CLI: change an agent on a card, add a skill, change a kind, draw an edge, approve the plan, restart.
