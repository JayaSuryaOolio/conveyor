# Learnings review (CV-81A)

Snapshot of the `learnings` table on 2026-11-04: 312 rows (214 proposed, 71 rejected, 19 applied, 8 accepted), from 3 repos
(Accounts 156, Conveyor 148, Oolio-one-gitops 5, Backoffice 3). Method: read every row, grouped by theme, checked the
testable claims against the code (`status.go`, `statusblock.go`, `dispatch.go`, `.gitignore`, `learning.go`, `brief.go`). I did
not re-read each source session transcript; where a verdict needs one, the run id is the row's `run_id`.

## How learnings get made (the pipeline)

1. `brief.go` tells every run to end with `learning: a non-obvious fact about this repo ... usually none`.
2. `statusblock.parseBody` appends any non-empty `learning:` value; `hook.go` inserts it unless the same repo already has the
   *exact* same body in any status.
3. A human accepts (-> `LEARNINGS.md` in `~/.conveyor/repos/<repo>/`, injected as `<ws>/CLAUDE.md`), rejects, or leaves it.

Every defect below traces to one of those three steps.

## Findings

### A. The pipeline lets junk in (fix in code)

| # | Defect | Evidence | Fix |
|---|---|---|---|
| A1 | The literal word `none` is stored as a learning (the template says "usually none", so agents write it). | ids 162, 163, 222, 308 | Drop `none`/`n/a`/`-` in `parseBody`. |
| A2 | Dedupe is exact-string only, so the same fact comes back reworded. | `node_modules` absent: ids 19, 27, 42, 47, 100, 130, 301 (one is already applied to SKILL.md, the rest keep coming). `go build ./...` needs `frontend/dist`: 168, 175, 176, 184, 189, 193, 223, 227, 241, 256, 260. Stale local `main`: 180, 210, 234. `sed -i` on macOS: 94, 145, 266, 269. Spotlight/graft stash-root: 188, 191, 224, 246, 292. `git push` to the stacked upstream: 116, 121. Wails/wailsjs: ~35 rows. | Not worth fuzzy matching. Fix the source: put the fact where agents already look (SKILL.md / CLAUDE.md) and the agents stop re-learning it. Reject the duplicates in bulk. |
| A3 | Agents never see what was already rejected or applied, so they re-propose it. | Rejected ids 19/47/100 (node_modules) were re-proposed as 130/301 and 297/298. | Put "known facts" in the brief: it already injects `LEARNINGS.md`; the repo SKILL.md is read too. The gap is that Conveyor's own repo has *no* `LEARNINGS.md` and no `CLAUDE.md`, so applied learnings land only in SKILL.md and agents ignore them. |
| A4 | No expiry: a learning true when written is never re-checked. | See "wrong" list. | Review before accepting, or link each to a file/path so it can be grepped. |
| A5 | 68% (214/312) is still `proposed`. The human queue is the bottleneck, not the agents. | counts above | Auto-reject A1/A2 classes, show only new facts. |

### B. Wrong or stale (verified against the code on main)

- **wailsjs committed vs gitignored** (contradictory pairs). `.gitignore:12` has `frontend/wailsjs/`, so rows saying "bindings are tracked/committed, hand-edit App.js/App.d.ts" are **wrong now**: 167, 170, 173, 235, 242 (and the "resolve by keeping both" advice). Rows saying "gitignored and generated" are right: 244, 249, 250, 254, 280, 294, 297, 298, 303. Rows 34, 114, 193 ("`generate module` drops hand-kept fields in models.ts") are moot for the same reason.
- **#52** "PR poller only checks tasks in review": `dispatch.go:280` lists running, needs_you and review. Wrong/stale.
- **#46** "this repo has no CLAUDE.md": still true on main (`CLAUDE.md` absent), but the premise of "so target SKILL.md" is the target the human already chose; not worth a learning.
- **#59, #97, #124, #129, #204, #208, #265, #266** (RTK hook): facts about the user's *personal* global hook, not the repo. They also contradict each other (#18 "use rtk read", #59 "rtk grep mangles output, use Read", #97 "put in a script"). #18 was *accepted* and is the worst case: it claims grep works without rtk, which this session's hook disproves. Reject all; if wanted, they belong in `~/.claude/CLAUDE.md`.
- **#8/#33 vs #296/#205**: same HOME-sandboxing fact, fine, but #33 says ImportState writes to the real `~/.claude/agents`; true, covered by SKILL.md "never write to the real ~/.claude/agents".
- **#265/#269/#94** (shell/sed gotchas) are generic, not repo facts.
- **Self-contradicting pair**: #95 (resume under 60k is cheap) vs #72 (fresh runs pay a ~50k floor): both are true, but they describe one fact; keep one.
- **#113** "workspaces/<KEY>/CLAUDE.md reaches the agent": true, and is how `LEARNINGS.md` is injected (`brief.writeLearnings`); rejected only because it looked like a brief instruction.
- **#292/#83B** "graft ships the spotlight CLI": already fixed (CV-83B renamed spotlight -> graft); stale.
- **#240** "app manifests already support service actions": true on the day, now in SKILL.md.

### C. Not learnings (recaps of one task)

About 40% of the rows are a one-off: "check the brief's PR has landed" (25, 30, 84, 156, 272, 117, 118), "this PR's counts did not match" (110, 111), "the brief named md-render/ but it is a file" (283), "C and D copied A and B" (172), per-PR design decisions (79, 80, 82, 83, 93, 125, 127), commit hygiene (63, 64, 135, 147). They fail the template's own test ("not a recap of this task"). The pattern "check X exists on main before starting" appears 8 times: that is one process rule (the brief should say which PR a task depends on), not 8 learnings.

### D. Right and worth keeping (examples, by repo)

- **Conveyor**: 196 (SQLite `SetMaxOpenConns(1)` deadlock), 255 (same for audit in a tx), 174 (resumed runs share session_id), 253 (chat transports re-created on every poll), 252 (`RunApp` blocks until exit), 261 (Slack buttons read `task_repos.pr_url`), 282 (comment requeue must be undone), 295 (`git log -G`), 302 (manifest checks `a.Name == "Graft"`), 311 (`epic check` only counts `epic_kind:build`), 275/274/219 (`spawn:` and `task set` cannot set repos), 243 (`task link` prints nothing), 276 (agents only learn commands from `relationsHelp`).
- **Accounts**: 1, 128 (usecase fixtures match bodies exactly), 153/155/291 (microsecond vs nanosecond on Linux CI), 164 (husky hook never runs in fresh worktrees), 165 (CI runs prettier on markdown), 181, 197, 207, 214, 262, 263, 264, 267, 304; oolio-one-gitops: 230-233, 236, 237 (preview env pins `ref=main`), 288.
- **Setup facts that recur but were *right***: `go build ./...` needs `frontend/dist`; wails lives in `~/go/bin`; fresh worktrees lack `frontend/node_modules` and `frontend/wailsjs`. Right, and the reason they recur is that the Conveyor repo has no `CLAUDE.md` for the first-five-minutes setup; one 6-line block would absorb ~60 rows.

## Verdict by status

| Bucket | Rows (approx.) | Action |
|---|---|---|
| Wrong/stale (B) | 25 | reject |
| Duplicates of an applied or accepted fact (A2) | 55 | reject |
| One-task recaps (C) | 80 | reject |
| Personal-env (RTK, sed, shell) | 20 | reject; add to `~/.claude/CLAUDE.md` if wanted |
| Keep (D) | 100-120 | accept to repo `LEARNINGS.md`/SKILL.md |

Numbers are counts of the themes above, not an exact partition; ids are the source of truth.

## Recommended changes

1. **Parser** (small, safe): ignore `learning: none` and variants in `statusblock.parseBody`. Test: one-liner in `statusblock_test.go`.
2. **Prompt**: replace the template line with something stricter, e.g. `learning: only if this repo had a trap that cost you >10 min and is not in CLAUDE.md/SKILL.md/LEARNINGS.md. Name the file or command. Omit the line otherwise.` Drops recaps, "check X on main", and personal-env facts. Needs a golden update (`internal/brief/testdata/brief.golden`).
3. **Setup block for Conveyor**: one `CLAUDE.md` (or SKILL.md section) for the recurring worktree setup (`frontend/dist`, `wailsjs`, `node_modules`, `~/go/bin/wails`, `origin/main`, graft). Kills A2's biggest clusters.
4. **Human queue**: add "reject all with the same first 40 chars as a rejected one", and a bulk-reject for the categories above.
5. **Brief**: dependencies on unmerged PRs should be a `blocks` link (SKILL.md already has `task link`), not a recurring learning.

## Out of scope here (suggest separate PRs)

Changes 1, 2 and 4 touch Go code and the golden file; this PR only adds the analysis. Say which to split out.
