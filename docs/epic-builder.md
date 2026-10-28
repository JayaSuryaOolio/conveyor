# Epic Builder: PRD and tech spec

Status: draft v2 (CV-4). v1 put an `epic_stage` column on tasks. v2 drops it: an epic is described with **labels**, and its order and hierarchy come from **task relations**. Open questions are in §3.

---

# Part 1: PRD

## 1. Problem

Conveyor runs one task at a time well. A piece of work the size of a Jira epic (several PRs, maybe several repos) still needs a human to:

- work out the requirements and acceptance criteria,
- split the work into tasks and put them in order,
- watch each task, copy PR review comments back to it, and check that the pieces add up.

That coordination takes the time. It is also hard to see where an epic stands, because the board is a flat list of cards.

## 2. What we're building

Two things on top of what Conveyor already has:

1. **An autonomous epic loop.** You describe a large task once. Conveyor plans it, runs it and fixes PR review comments until it is merged. It only asks you something when it has to.
2. **The Epic view.** A flow chart of the task tree, drawn from relations, with live status on every card.

There is no new building block. An epic is an ordinary task:

| Concept | How it's expressed | Already exists? |
|---|---|---|
| "This task is an epic" | label `epic` on the root task | labels (#131) |
| Stage of the epic | label `epic_stage:<stage>` on the root, one at a time | labels |
| Kind of child ticket | label `wayfinder:<type>` (decision tickets) or none (build tickets) | labels |
| Hierarchy | `parent_id` (sub-tasks) | yes |
| Order | `blocks` links; the dispatcher already holds blocked tasks back | yes |
| "Wants you" | status `needs_you` | yes |

The flow chart is these relations drawn as a picture. The board stays the source of truth.

### 2.1 Planning with wayfinder

Planning follows Matt Pocock's [`wayfinder`](https://github.com/mattpocock/skills/blob/main/skills/engineering/wayfinder/SKILL.md) skill ([background](https://latent.space/p/wayfinder-skill)). It fits Conveyor almost one to one:

| wayfinder | Conveyor |
|---|---|
| map issue, label `wayfinder:map` | the epic task (label `epic`); its description holds Destination / Notes / Decisions so far / Not yet specified / Out of scope |
| ticket = child issue, label `wayfinder:research\|prototype\|grilling\|task` | sub-task with the same label |
| native blocking | `conveyor task link A blocks B` |
| frontier = open, unblocked, unclaimed children | children in `todo` with no open blockers, which is exactly what the dispatcher launches |
| claim = assign | the dispatcher starting a run |
| HITL ticket (grilling, prototype) | the run asks its questions and goes to `needs_you`; your comment resumes it |
| AFK ticket (research, task) | runs with no human |
| resolution comment + close + pointer on the map | status block summary, task `done`, the agent appends one line to the epic's "Decisions so far" |
| "one ticket per session" | one task per run, already how Conveyor works |

wayfinder reads its tracker operations from a "tracker doc". We ship one that maps them to the `conveyor task` CLI (§8), so the skill runs unchanged.

### 2.2 The loop

```
 chart ──► decide ──► spec ──► build ──► verify ──► done
   │         │          │        │          │
 epic run  wayfinder  epic run  build     epic run
 (charts   tickets    (writes   sub-tasks  (checks AC;
  the map)  (frontier  spec +    (TDD, PR,  gaps → new
            runs by    creates   review     build
            itself)    build     loop)      tickets)
                       tickets)
```

| Stage | What happens | Moves on when | Human? |
|---|---|---|---|
| `chart` | The epic run grills you on the destination and the acceptance criteria, then creates the decision tickets and their `blocks` links (wayfinder "Chart the map"). | Map written | Yes: grilling. This is the main conversation. |
| `decide` | Frontier tickets run on their own as they unblock. Each resolution can add tickets or turn fog into tickets. | No open `wayfinder:*` children | Only on grilling/prototype tickets |
| `spec` | The epic run writes the spec from "Decisions so far", creates build sub-tasks (repo, AC, agent) and `blocks` links, and moves them to todo. | Build tickets exist | No, unless the gate in §3 Q1 is on |
| `build` | Build tickets run test-first and open PRs. Review comments are picked up every 5 min (§10) and fixed. | Every build child done or cancelled | Only if an agent gets stuck, or for merge (§3 Q2) |
| `verify` | The epic run checks every AC against the merged work. Gaps become new build tickets and the stage goes back to `build`. | All AC met | No |
| `done` | The epic moves to done. | | |

The epic agent swaps its own stage label, so moving between stages needs no Go code. The one new dispatcher rule: when the last open child settles, wake the epic (§9).

## 3. Open questions

1. **AC sign-off gate.** Should the run stop for an explicit "approve" after `chart` (AC and map written) and after `spec` (build tickets created)? Proposal: one gate only, after `chart`, controlled by a per-epic label `epic_gate:off` to skip it. The rest runs AFK.
2. **Merging.** Who merges build PRs? Proposal: the human, as today. An `epic_automerge` label lets the agent run `gh pr merge --auto` once the PR is approved and checks are green.
3. **PR fields on cards.** Showing the review decision and CI checks on cards means storing them. Proposal: two nullable columns on `task_repos` (`pr_review`, `pr_checks`), filled by the existing poll. These are PR cache columns, not epic state. The alternative is no columns and PR state only.
4. **Graph library.** `@xyflow/react` (pan/zoom/minimap, the n8n look, one dependency) or hand-rolled SVG? Proposal: React Flow, with our own layering (no dagre).
5. **Label names.** Reuse wayfinder's `wayfinder:*` names as they are (proposal; it keeps the skill unchanged), or rename them to `epic:*`?

## 4. Requirements

**Epic loop**
- R1. Adding the label `epic` to a root task turns it into an epic. If no `epic_stage:*` label is present, the stage is `chart`. "New task" gets an "Epic" toggle that just adds the label.
- R2. `conveyor task label KEY set epic_stage:<s>` replaces any other `epic_stage:*` label in one transaction. More generally, `set` with a `prefix:` replaces labels that share the prefix.
- R3. The epic agent template follows the loop in §2.2 and uses the wayfinder skill with the Conveyor tracker doc.
- R4. Decision tickets carry `wayfinder:<type>`. HITL types end their run in `needs_you` with numbered questions, at most 5 per batch.
- R5. Build tickets carry the epic's AC and a link to the spec, written into their description by the epic run, and are told to work test-first.
- R6. While any child is running or todo, the epic itself does not run. When the last open child finishes, the epic is requeued with a system comment that names its stage.

**PR review polling**
- R7. Every 5 minutes, for each task in `review` with an open PR, new review bodies and inline comments are posted to the task thread as one system comment, which requeues the task. The poller skips comments that start with `ai agent:`, comments already seen, and comments on resolved threads.
- R8. The poller is one place in the dispatcher's tick: no goroutine or timer per task, a timeout on each `gh` call, and it stops when the app context is cancelled (§10).

**Epic view**
- R9. A page per tree. It opens from any task and resolves to the root by walking `parent_id`. Layout: children are grouped under their parent, and columns follow the `blocks` order.
- R10. Each card shows: key, title, labels (stage/type chip), task status with a pulse while running, a worktree chip (present/missing), and a PR chip (state, review decision, checks, unresolved comment count). Each chip links out: PR → browser, worktree → shell or editor (existing quick actions), card → task page.
- R11. A header strip shows the stage pipeline (`chart → … → done`) with the current stage highlighted, plus `n/m` children done and a "wants you" count.
- R12. Updates are live on `db:changed`. There is no refresh button.
- R13. Board cards and the task page header get a flow icon that opens this view. Every task gets one; a lone task shows as one node plus its link neighbours.
- R14. A sidebar entry "Epics" lists tasks labelled `epic` (the board label filter from CV-54 may already cover this; see §11).

## 5. Out of scope (v1)

- Editing the graph by dragging (drawing an edge = `task link`). It can come later.
- Saved node positions. Custom stage lists per epic.
- Jira import or sync.
- Worktree detail (dirty, ahead/behind). It costs a git call per card; the chip shows present/missing only.

## 6. Success

- A one-paragraph epic reaches merged PRs, with the human only answering grilling questions and reviewing PRs.
- From the Epic view you can tell in five seconds the stage, what is blocked, what wants you, and which PR has unread review comments.

---

# Part 2: Tech spec

## 7. Data model

No new tables. Epic state is labels plus existing relations.

- `labels` / `task_labels` (#131): `epic`, `epic_stage:<s>`, `wayfinder:<type>`, optional `epic_gate:off`, `epic_automerge`.
- `parent_id`: hierarchy. `task_links(kind=blocks)`: order (loops are already refused, see `TestBlockLoops`).
- Review watermark: an `events` row `pr_review_seen` with detail `<repo_id>:<latest comment id>`. No column.
- Only if §3 Q3 is accepted: `ALTER TABLE task_repos ADD COLUMN pr_review TEXT; ADD COLUMN pr_checks TEXT;` through an idempotent `migrateAddX`.

`store.SetPrefixedLabel(db, taskID, "epic_stage:build")` deletes the task's labels that match `epic_stage:%` and adds the new one, in one transaction. About 15 lines next to `AddLabel`.

## 8. Epic agent and wayfinder

- **Agent template** `epic` (Agents page preset / `conveyor agent add epic --template epic`). The prompt describes §2.2 and reads the stage from the brief's label list. Stage logic lives in the prompt, not in Go, so it can change without a release.
- **Skill:** the wayfinder skill is installed in the user's skills. We do not vendor it.
- **Tracker doc** `docs/wayfinder-conveyor.md`, referenced from the agent prompt. Its "Wayfinding operations" section:

  | operation | command |
  |---|---|
  | create map | the epic task itself; `conveyor task set KEY --desc` for the body |
  | create ticket | `conveyor task new "q" --parent KEY --repo R --desc "## Question …"` then `conveyor task label NEW add wayfinder:<type>` |
  | wire blocking | `conveyor task link A blocks B` |
  | frontier | `conveyor task show KEY --json` → children in todo with no open blockers |
  | claim | none: the dispatcher's run is the claim |
  | resolve | status block `done`; summary = resolution; append to the map's Decisions so far |
  | out of scope | `conveyor task cancel` + one line in the map |

- **Brief:** `internal/brief` already lists relations (parent, children, links) but not labels. Add a `labels:` line, plus one line when the root has `epic`: "Epic stage: <s>. Children: n/m done, k wants you." Update the golden test.

## 9. Waking the epic

Nothing wakes a parent today when its children finish. Add one check to the tick:

- Find root tasks labelled `epic` whose status is not done/cancelled/running and that have no children in `todo`, `running` or `needs_you`. If such an epic has at least one child that finished after the epic's last run ended, requeue it with a system comment: `all sub-tasks settled (stage <s>)`.
- This is one SQL query per tick. The epic's run then decides the next stage.

## 10. PR review polling (R7, R8)

**Where:** `Dispatcher.Tick`, next to `pollPRs`, using the same pattern as `tickAutomations`:

```go
const reviewPollEach = 5 * time.Minute

func (d *Dispatcher) pollReviews(ctx context.Context) {
    if !d.lastReview.IsZero() && d.now().Sub(d.lastReview) < reviewPollEach ||
        !d.reviewBusy.CompareAndSwap(false, true) {
        return // not due, or the previous pass is still running
    }
    d.lastReview = d.now()
    go func() {
        defer d.reviewBusy.Store(false)
        for _, t := range reviewTasks(d.DB) {      // status=review, open PR
            if ctx.Err() != nil { return }
            cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
            err := gh.SyncReviews(cctx, d.DB, t)   // fetch since watermark, post, requeue
            cancel()
            if err != nil { d.logf("review poll %s: %v", t.Key, err) }
        }
    }()
}
```

Why this can't leak or go stale:
- There is one ticker for the whole app (the dispatcher's existing one). Nothing is scheduled per task, so a deleted, cancelled or done task has nothing to clean up. It just drops out of the `status=review` query on the next pass.
- `CompareAndSwap` allows at most one pass in flight. A slow GitHub never piles up goroutines.
- Every `gh` call runs under a context with a 30 s timeout. `exec.CommandContext` kills the process when that context ends, and app shutdown cancels the parent context.
- Nothing is held in memory between passes. The watermark lives in the DB, so a restart neither reposts comments nor misses them.

**Fetching** (`internal/gh`): `gh api repos/{o}/{r}/pulls/{n}/reviews` and `.../pulls/{n}/comments`, filtered to `id > watermark`, bodies not starting with `ai agent:`, and not by the PR author when the PR author is the agent's account. Unresolved-thread filtering uses `gh api graphql` `reviewThreads(isResolved)`, in the same call that counts unresolved comments for the card chip.

**Posting:** one system comment, `PR review on <url>:` followed by `> file:line` + body per comment. `AddComment` + `UpdateTaskStatus(todo)` follow the existing comment-requeue path, so debounce (`comment_debounce_secs`) applies.

**Tests:** a fake `gh` script (as in `gh_test.go`) that returns reviews; assert one comment is posted and that a second pass posts nothing. A dispatcher test with a fake clock asserts there is no second pass inside 5 min and no overlap while `reviewBusy` is set.

## 11. Epic view (frontend)

**Binding** (`internal/ui/app.go`, thin over `core`):

```go
type EpicGraph struct {
    Root  string      `json:"root"`
    Nodes []GraphCard `json:"nodes"` // TaskCard + Running, Worktree bool, PRs []PRChip
    Links []store.Link `json:"links"` // blocks/relates among nodes
}
type PRChip struct{ URL, State, Review, Checks string; Unresolved int }
func (a *App) EpicGraph(key string) (EpicGraph, error) // any key in the tree; resolves to root
```

`GraphCard` embeds the existing `TaskCard` (which already has Parent and Labels). The worktree check is `os.Stat(workspace.PrimaryPath(task))`.

**Layout** (`frontend/src/epic.tsx`, one file, about 40 lines of layering):
- Group nodes per parent, with children inside.
- Within a group, column = longest `blocks` path from a child that has no blockers inside the group; row = order by key. A `blocks` edge that crosses groups is drawn but does not affect columns.
- Edges: `blocks` solid with an arrow, `relates` dashed.
- Stage strip above the canvas, read from the root's `epic_stage:*` label.

**Navigation:** `App.tsx` view `{name:'epic', key}`; shortcut `e` on a selected task; an `IconSitemap` ActionIcon on board cards (`components.tsx`) and in the task page header (`views.tsx`). "Epics" in the sidebar = the board filtered by `label:epic` if CV-54's deep-link filter covers it; otherwise a small list.

## 12. CLI

```
conveyor task label KEY set <prefix:value>   # new: replace same-prefix labels (R2)
conveyor task graph KEY [--json]             # new: EpicGraph as a text tree, for agents and debugging
```

Everything else (`new --parent`, `link`, `label add|rm`) exists.

## 13. Delivery plan (sub-tasks)

| # | Sub-task | Depends on |
|---|---|---|
| A | PR review polling every 5 min (§10). Useful for every task on its own. | — |
| B | `SetPrefixedLabel` + `task label set` + epic wake-up check in the dispatcher (§7, §9) + `task graph` | — |
| C | Epic agent template + Conveyor tracker doc for wayfinder + brief line + golden test (§8) | B |
| D | Epic view: binding, layout, cards with PR/worktree/status chips, icons, stage strip (§11). Needs §3 Q3/Q4 answered. | B (A for the review/unresolved chips) |
| E | Dogfood one real epic end to end, then fold findings into the agent prompt and this doc | A, C, D |

## 14. Testing

- Store: `SetPrefixedLabel` replaces only the same prefix.
- Dispatcher: epic wakes when its children settle and not before; review poll cadence and no overlap (fake clock).
- gh: review fetch, watermark, `ai agent:` skip, resolved-thread skip.
- Brief: golden test for an epic root.
- UI: `npm run build`; a manual check in `wails dev` with a tree seeded through the CLI.
