# BRIEF.md template (CV-184G, AC7 of CV-184)

## Does a template exist?

Yes, in code: `brief.Build` (`internal/brief/brief.go`) writes the sections in a fixed order. Before this change only Decisions (CV-184B) had a size cap. Description, acceptance criteria, latest run state/next, run summaries, human comment lines and Relations had none: a pasted spec or an epic root with 80 children grew the brief without limit.

## What other tools do

| Source | Practice | Taken |
|---|---|---|
| multica (`docs/research/landscape.md` §2.3–2.4) | Stable per-task brief, volatile per-run data after it; pointers, not data ("run `X get`, expand a thread on demand") | Each cap ends in a pointer command (`conveyor task show`, `task graph`, `task decision list`) |
| multica case study | A 2–4K-token brief replaces 90–150K of resumed context | Normal briefs stay in that range; the cap bounds the worst case |
| hermes-agent (§5) | Memory capped in characters (2,200 / 1,375), frozen per session | Fixed per-section caps in characters, not a token estimate |
| hermes kanban | Worker context = title, body, prior attempts, comments, parent results | Same shape: Description, Latest/Recent runs, Human comments, Continues |
| CV-117B/I study | Decisions got lost; brief rules were taken literally | Decisions sit above Description (CV-184B) |
| Conveyor design §6 | Effective context is about 60k tokens | Whole brief at most about 24 KB (about 6k tokens, 10%) |

## The template

Fixed order; a `[...]` section appears only when it has content. Caps are in runes, except the Decisions total.

| # | Section | Cap |
|---|---|---|
| 1 | Header: title, key, labels, epic/stage, kind rule, skills | short, fixed |
| 2 | Repos | one line per repo |
| 3 | [Decisions] | 240 per line, 4000 bytes total, oldest dropped with a count |
| 4 | Description | 6000, then a `conveyor task show` pointer |
| 5 | [Continues KEY] (follow-ups) | parent description 2000, criteria 1500, history as below |
| 6 | Acceptance criteria | 1500 |
| 7 | [Other repos' context] | 30 entries, 100 per description (existing) |
| 8 | Latest run, Recent runs | 300 per line, last 3 runs |
| 9 | Human comments | last 20, first line only, 200 each |
| 10 | Relations + split help | 30 lines of 160, then a `conveyor task graph` pointer |
| 11 | Status block, Rules, Older history | fixed text plus `brief_extra_rules` |

`TestTemplateCaps` builds a task with every field 50 KB long and checks the order and the budget (worst case 21.7 KB).

## Not changed

- Section order. Moving the fixed text (status block, rules) above the per-run sections would help the prompt cache only if the brief were a prompt prefix. It is read through a tool call, so it was left as is.
- `brief_extra_rules` and app `inject.brief` are user-supplied and not capped.
