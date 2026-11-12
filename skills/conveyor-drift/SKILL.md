---
name: conveyor-drift
description: Check Conveyor's docs (docs/guide.md, .claude/skills/conveyor/SKILL.md, CLAUDE.md) against the code - CLI commands and flags, settings and their defaults, named functions and files - and open one PR that fixes the mismatches, or report none. Use for the weekly "Conveyor docs: drift check" task, or when asked to check Conveyor's docs for drift.
---

# Conveyor doc drift

You check that Conveyor's docs still describe the code, and fix them where they don't. Modelled on `egm-drift-report` (google/elephant-goldfish-model). You usually run as the weekly Conveyor task, in a worktree of the Conveyor repo.

The code is the truth. Change the docs to match it; never change code to match a doc. If a doc describes something the code lacks and it looks like a missing feature rather than stale text, don't delete it: list it in your report and leave the doc as is.

## Docs

1. `docs/guide.md`
2. `.claude/skills/conveyor/SKILL.md`
3. `CLAUDE.md` (repo root)

Do them one at a time, so tool output stays small.

## For each doc

1. **Read it.** List every checkable claim: command and subcommand names, flags, setting keys and defaults, file paths, Go identifiers (`dispatch.sessionDirective`, `resume_threshold`), labels, statuses, cron schedules.
2. **Find the code it names.** Use `grep -rn` (or `rg`) for each name. Sources of truth:
   - Commands: `go run ./cmd/conveyor` (no args) prints the command list; `register(` calls in `cmd/conveyor/*.go`. Subcommands and flags: the `switch` arms and `flag.NewFlagSet` / `fs.String|Bool|Int` calls in the matching `cmd_*.go`.
   - Settings and defaults: `internal/store/settings.go`.
   - Functions, types, files: grep the identifier; a path must exist (`ls`).
   - Brief and prompt text: `internal/brief`, `internal/dispatch`.
3. **Compare.** Report only differences:
   - a name, flag, setting or path the doc gives that the code doesn't have (renamed, removed, never built);
   - a default or value that differs (e.g. a timeout, a schedule);
   - behaviour described differently from what the code does;
   - a user-facing command, flag or setting the code has that no doc mentions (guide.md only).
   If unsure, read the code again rather than guessing. Don't report wording, style or things that match.

## Fix and report

- Fix every confirmed divergence in the doc itself, keeping the doc's tone and terseness. One commit, one PR per the brief; cite the doc section and the code location (`file:line`) for each fix in the PR body.
- Run `go build ./... && go test ./...` if you touched anything but Markdown (you shouldn't have).
- No divergences: open no PR, and say "No drift detected" in your summary.
- Divergences you didn't fix (missing features, unclear intent) go in the PR body and the summary as a list: `doc:section - claim - what the code does`.
