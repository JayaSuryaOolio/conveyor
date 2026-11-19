# Study: why CV-117B and CV-117I took so many rounds

Source: `conveyor task show CV-117B --thread --json` (Accounts PR #1493) and the same for CV-117I (PR #1492), read 2026-11-19.

## Short answer

The approach was never agreed before coding. Both tasks looked too small to need it, so the human left it to the agent. The agent then chose the module layout, names and filter shape alone, and the human and the reviewer corrected them one PR round at a time. The harness made it worse in two ways. It forgot decisions between runs, so a fresh session undid a choice made for a good reason. And it let the agent treat a human reviewer's request like a bot finding it could decline.

## Corrections, one by one

### CV-117B (skip deactivated locations on business-wide copy)

| # | When | Human correction | What the agent did wrong | Pattern |
|---|---|---|---|---|
| 1 | 11-17 08:12 | "I was asking if it was the case now" | Read a question ("does relationships drop the relation on deactivation?") as a proposal and argued against deleting tuples. | Misread intent |
| 2 | 10:56 | "Use accounts adapter **in subscriptions** ... start by adding an accounts port" | Put the adapter in `account/adapters`, the opposite module, reading the account repo directly. | Approach not agreed, misread instruction |
| 3 | 11:13 | srinivasaleti review: `AccountService` port, `subscription/adapter/accounts.go` | Declined it: "the port already exists as `domain.Locations`". The rename was done anyway on 11-18. | Human reviewer treated as a bot |
| 4 | 11:26, 11:40 | "Why does the adapter call the DB?", "Why so many iterations?" | Patched the wrong shape each round (new query, then rename) instead of moving the adapter. The agent said so itself at 11:46. | Patching instead of re-planning |
| 5 | 15:01 | "Check the human comments" | Two inline comments from srinivasaleti never reached the run. | Harness: review watermark (fixed by CV-195, #315) |
| 6 | 15:20 | "`ActiveOnly` bool is amateur, use a status slice"; "fetching 1000 is hacky" | Added a bool and a Go-side filter, missing `ListLocationInput.Statuses`. The 09:19 run had named that field. | Missed existing capability, bool instead of slice |
| 7 | 16:07 `#fresh` | "Maybe the agent filtered locally because of no-status locations" | The Go filter existed **for a reason**: legacy rows with no `status` count as active, and the store's `#status IN (...)` skips them. That was explained at 09:19 (Trap 1) and 15:07. After the switch at 15:25, the agent raised it again as a "Decision for you" at the end of a long reply. The human asked for the hacky path back. | Decision not durable, on both sides |
| 8 | 11-18 05:38 | Reviewer: drop the `if`, move the lookup below it, it runs on every PATCH | The lookup ran even for business-level features. No bot or run caught it. | Not self-reviewed against the call path |

### CV-117I (cancel the subscription on business deactivation)

| # | When | Human correction | What the agent did wrong | Pattern |
|---|---|---|---|---|
| 1 | 11-16 07:59 | "`Live()` should allow past_due, undo that" | Answered the literal question ("`Live()` is unchanged") and did not link it to its own earlier note that active-only reads hide `past_due`. | Answered the words, not the intent |
| 2 | 10:13 | "The PR description says the contrary; don't block ChangePlan" | Followed the brief's "every read filters by `status: active`" literally, so `ChangePlan` on `past_due` returned 404. | Brief rule taken literally, specs read late |
| 3 | 10:50 | "Did the Review Webhooks app catch my comments?" | Four human inline comments with no new push started no run. | Harness: review delivery (CV-195) |
| 4 | 11-17 11:01 | "Suspended is temporary, cancel instead" | Did what the brief said. The status meaning was a product decision made late, and the PRD (`docs/superpowers/specs/subscriptions/prd.md`) already described the statuses. | Approach not agreed (human side) |
| 5 | 10:15, 10:54 | "Yes make the changes", "Yes, post them" | Left commits local and asked before pushing or replying. Each question cost a round. | Unneeded permission asks |

CV-117I was mostly decisions made late by the human. CV-117B was mostly the agent choosing structure alone, plus lost decisions.

## Patterns, with what the harness does today

1. **Decisions are not durable.** `brief.Build` (`internal/brief/brief.go`) carries the last 3 run summaries on one line each, and only the **first line** of each human comment (`history`, `maxComments = 20`). Agent replies are not carried at all. A `#fresh` run therefore never sees "no-status rows count as active", and the human, reading long replies, lost it too. The epic flow already has "Decisions so far" on the epic root (`docs/epic-builder.md`); plain tasks have nothing.
2. **No approach checkpoint.** A plain task goes straight from brief to code to PR. The layout question in CV-117B (which module owns the port and adapter, query or repo, names) was settled over about 8 PR rounds instead of one 5-line plan.
3. **Existing APIs and repo idioms missed.** `ListLocationInput.Statuses` existed, and the repo filters with slices, not bools. The agent knew the field at 09:19, and a later session forgot it (pattern 1). Nothing asks for a "what I'm reusing" check before coding.
4. **Human reviewers handled like bots.** The review prompt is the same for everyone: "Fix the valid findings. For one that is wrong or out of scope, reply ... then resolve it." That invites the agent to argue with a human reviewer's design request (B#3). The agent also answered questions with arguments (B#1, I#1).
5. **Review delivery gaps.** Human inline comments were missed twice (B#5, I#3). This is the per-type watermark bug fixed in CV-195 (#315, #316). No new fix is needed beyond confirming it on a live PR.
6. **Decisions buried in long replies.** "Decision for you" sat at the bottom of 15-line replies (B#7, I#1), so the human missed it, and the next run did not know about it either.

## Proposed harness fixes

| Fix | Where | Patterns |
|---|---|---|
| **Decisions section in BRIEF.md.** A status-block line `decision: <what> — <why>` is stored on the task. The brief renders every decision under `## Decisions`, never truncated, above Description. The human can add or strike one from the task page or CLI. | Conveyor: `internal/brief`, status block parser, store | 1, 6 |
| **Approach step for non-trivial tasks.** The first run posts an Approach: files and layer, existing functions and types it will reuse (with paths), new names, and open decisions. It then ends `needs_you` until the human acks. Skip it with a `small` label, or when the description already states the approach. The ack turns the approach into Decisions. | Conveyor: brief rules, `prompt_fresh`, label handling | 2, 3 |
| **Review prompt splits human from bot.** For a human reviewer: treat the request as the target. If you disagree, ask in the thread and do not decline or resolve it. Answer a question in its first line before giving options. Bots keep the current "fix valid, reply and resolve" rule. | Conveyor: `reviewHeader` in `internal/gh/review.go` | 4 |
| **Reply format: decision first.** A reply that needs a human decision starts with `Decision needed:` on its first line. Push and reply without asking when the human already said to make the change. | Conveyor: brief rules | 6, I#5 |
| **Repo skill: cross-module and filter idioms (Accounts).** In `.claude/skills/backend`: the consumer module owns the port and adapter; the adapter takes the other module's query as an injected dependency, wired in `cmd/server.go`; list filters are slices (`Statuses []LocationStatus`), never bools; check the repo's `List*Input` before filtering in Go. | Accounts repo | 3 |

Already done: CV-195 / #315 and #316 (missed inline comments).

## The human's side (from the CV-184 comments)

Root cause, per the human: the dev expected the agent to own the task end to end, because standards live in the codebase and skills. They did not decide the approach themselves. The fixes above make the harness ask. These habits keep the human in touch with the code:

- **Own the approach, delegate the typing.** Spend about 5 minutes on the Approach note: which module, which existing API, what names. Approving it is the one thing only you can do. For CV-117B this would have saved most of the rounds.
- **Read the first diff yourself, before the bots.** Bot rounds polish whatever shape exists. A human glance at round 1 catches shape problems such as the wrong module, a bool flag, or a lookup on every call.
- **State decisions as decisions.** "Decision: X because Y" goes into Decisions and survives `#fresh`. Mark a question as a question, so it is not read as an order.
- **Turn style corrections into skill lines.** "Use a status slice, not a bool" corrected one PR. Ask the agent to add it to the repo skill, and the next task follows it.
- **Batch feedback.** One comment with all points, not one per run. Each comment is a run, and each run re-derives context.
- **Use `#fresh` only after decisions are recorded.** A fresh session starts with what BRIEF.md holds and nothing else.

## Task structure

Not every task needs spec, spec review and plan. Scale the gate to the task. The epic flow already covers large work.

| Size | Flow |
|---|---|
| small (label `small`, typo or one-line fix) | code → PR |
| default | Approach note → human ack (becomes Decisions) → code → self-review against the repo skill → PR |
| large | epic (`docs/epic-builder.md`) |
