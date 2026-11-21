---
name: write-migration
description: Write a one-off data migration or backfill script safely - choose API calls vs direct DB writes, load config from .env.<name> with the env as an argument, dry-run by default and --apply to write. Use when asked to write, review or run a data migration, backfill, bulk fix or one-time script that changes records.
---

# Write a data migration

A migration changes real data once and is hard to undo. Make the script boring and safe. Start from `example/migrate.sh` (copy it next to the code that needs it).

## 1. API or direct DB?

Default to the service API. Go direct to the DB only when an API call cannot do the job.

| | API | Direct DB |
|---|---|---|
| Validation, auth, audit log | kept | skipped |
| Events, webhooks, caches, search index | fire correctly | silently stale |
| Schema coupling | none (contract) | tied to table layout |
| Speed on huge volumes | slow, rate limited | fast |
| Undo | usually a reverse call | needs a backup |

Use direct DB only if: no API exists for the change; or volume makes the API impractical (state the numbers); or it is a pure column backfill with no side effects. Then write the reason at the top of the script, take a backup or snapshot first, and run each batch in a transaction.

If unsure, do a one-record API call on dev and see what side effects it triggers: if there are any, a DB write would skip them.

## 2. Env and arguments

- First argument is the env name; the script loads `.env.<name>` (`.env.dev`, `.env.staging`, `.env.prod`). No default env, and a missing file is a hard error.
- Keep `.env.*` out of git (check `.gitignore`). Commit a `.env.example` with keys and no values.
- Never print secrets or full connection strings; print the env name and host only.
- `prod` needs an extra typed confirmation before `--apply`.

## 3. Dry-run by default

- No flag = dry-run: read only, print what would change (id, old, new) and totals. It must never write, call a mutating endpoint or send a notification.
- `--apply` is the only way to write. Print the mode (`DRY-RUN` / `APPLY`) and env at the start and the counts at the end.
- Run order: dry-run on dev, apply on dev, check, then repeat for staging and prod.

## 4. Other rules

- Idempotent: running twice is a no-op. Select only rows still needing the change, or skip already-migrated ones.
- Row-count guard: take `--max N` (expected count) and abort if the selection is bigger. This catches a bad filter.
- Batch with a pause; on error stop (don't skip silently) and print the last id so the run can resume.
- Log each change with id, old and new value to a file, so there is a rollback path. Take the old values before writing.
- Select by explicit filter or id list, never "all rows". No hardcoded ids, URLs or secrets in the script.
- Verify after apply: re-run the dry-run (expect 0 to change) and spot-check a few records.
- Review the dry-run output in the PR before anyone runs `--apply`. Commit the script with the change that needs it; delete it once it ran everywhere.
- Notifications, emails and webhooks to real users: disable or point at a sink for the run.
