# Planner adapter: wayfinder

Maps Matt Pocock's [wayfinder](https://github.com/mattpocock/skills/blob/main/skills/engineering/wayfinder/SKILL.md) onto Conveyor's epic labels. The default planner (`epic_planner:wayfinder`, or no `epic_planner:` label). Another planner is another file in this folder with the same table; no Go changes.

| wayfinder | Conveyor command |
|---|---|
| map issue | the epic root. `conveyor task set ROOT --desc "..."` with sections Destination, `## Acceptance criteria` (AC1, AC2, ...), Decisions so far, Not yet specified, Out of scope |
| research ticket | `conveyor task new "..." --parent ROOT --repo R --desc "..."`, then `conveyor task label KEY add epic:<name>` and `conveyor task label KEY set epic_kind:research` |
| prototype ticket | same, `epic_kind:prototype` |
| grilling ticket | same, `epic_kind:grilling` |
| task ticket | same, `epic_kind:build`, plus `--agent A`, the AC ids and spec link in `--desc` |
| native blocking | `conveyor task link A blocks B` |
| frontier | children in `todo` with no open blockers; the dispatcher launches exactly these. Put a ticket on the frontier with `conveyor task move KEY todo` |
| claim | the dispatcher starting a run; never claim by hand |
| HITL ticket (grilling, prototype) | the run asks in its final reply without a status block and waits in `needs_you`; the human's comment resumes it |
| AFK ticket (research, build) | runs with no human |
| resolution | the child's status block summary |
| close | the child moves to `done` |
| pointer on the map | one line appended to the root's Decisions so far (`conveyor task set ROOT --desc ...`), plus `conveyor epic log ROOT "<why>"` |
