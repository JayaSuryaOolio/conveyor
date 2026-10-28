# Troubleshooting

Each recipe names a symptom, how to confirm it, and the fix. Prefer the fix that goes through Conveyor (`conveyor hook`, `conveyor task ...`) over editing `conveyor.db` by hand: those paths run the same parsing and status moves a normal run does. The quick checks (nothing starts, untrusted workspace, logs) are in the [guide](guide.md#troubleshooting).

To have an agent do it, run `claude "/conveyor-doctor fix CV-9"` (or `fix` with no key to scan every task) in the app's integrated terminal, or comment that request on any task that is not running. A comment on the stuck task itself only queues, because the task is still running.

When you find a new failure mode and a manual fix for it, add it here. When the dispatcher learns to fix it on its own, keep the recipe and say since when it is automatic.

## A run stays "running" after its session finished

Seen on CV-9 run 13. The session was resumed with `claude --bg --resume <id> "<prompt>"`. Claude Code woke it **without the options it was started with**: it got a new name, no `--settings <home>/claude-settings.json`, and so no `conveyor hook Stop`. The agent finished and printed its status block, but Conveyor never heard about it.

**Confirm**

```sh
conveyor task show KEY --runs                  # last run: running
conveyor task sessions KEY                     # its session id
claude agents --json --all                     # that sessionId: "state": "done", "status": "idle"
grep KEY ~/.conveyor/logs/hook.log             # no Stop for that session
```

**Fix.** Replay the Stop hook from the session's transcript. Conveyor then parses the status block, finishes the run and moves the task, exactly as if the hook had fired:

```sh
SID=<session id>
CWD=~/.conveyor/workspaces/KEY/<primary repo>   # the session's cwd from `claude agents --json --all`
T=$(ls ~/.claude/projects/*/$SID.jsonl)
python3 - "$SID" "$CWD" "$T" <<'EOF' | conveyor hook Stop
import json, sys
sid, cwd, path = sys.argv[1:]
last = ""
for line in open(path):
    e = json.loads(line)
    if e.get("type") == "assistant":
        for c in e["message"].get("content", []):
            if isinstance(c, dict) and c.get("type") == "text":
                last = c["text"]
print(json.dumps({"session_id": sid, "cwd": cwd, "transcript_path": path, "last_assistant_message": last}))
EOF
conveyor task show KEY --runs                  # run finished with its summary
```

**Prevented since CV-47.** Conveyor now passes `--settings <home>/claude-settings.json` on resume too. If a resumed run still never ends and `hook.log` has no `UserPromptSubmit`/`Stop` for its session (seen on CV-46 run 148, where the resume started "a copy as" a new session id), check that `<home>/claude-settings.json` exists and replay the Stop hook as above.

Before that, Conveyor did not detect this on its own: it is a Claude Code resume bug, and checking every session each tick costs more than the rare case is worth. The `conveyor-doctor` skill finds and fixes it on demand (see the top of this page).

## A resumed run is adopted by a new session id

Seen on CV-46 run 148 and CV-56 run 183; the architecture is in [session-identity.md](session-identity.md). `claude --bg --resume` started the work as a copy under a new session id. Hooks fire, but they name an id the resumed run does not have, so Conveyor creates an `interactive` run for them and the resumed run never gets its `Stop`: the task stays `running` after the agent finished.

**Confirm**

```sh
conveyor task show KEY --runs --json     # last auto/resume run: running, bg_id != first 8 chars of session_id
                                         # plus an interactive run whose session_id starts with that bg_id
claude agents --json --all               # that bg_id's sessionId is the NEW one; state done / status idle
grep KEY ~/.conveyor/logs/hook.log       # UserPromptSubmit/Stop for the new session id
```

**Fix.** Only once the session is idle (a live session fires its own `Stop`, and replaying one mid-turn ends the task early). Replay the Stop with the *original* run in mind: the replay lands on the run whose `session_id` matches, which is the interactive one, so it finishes that run and moves the task, but the resumed run stays `running` until the reaper fails it when the session is gone.

```sh
# same replay script as the recipe above, with SID = the NEW session id
conveyor task sync KEY
conveyor task show KEY --runs --json     # task moved; interactive run finished
```

Do not edit the DB to re-point `session_id`. If the resumed run is still `running` after its session has gone, the reaper fails it ("session vanished"): read the interactive run's summary for the real outcome.

**Not yet prevented.** The proposed fix (update `session_id` from the agents list in the reaper, never create an interactive run for a known `bg_id`) is in [session-identity.md](session-identity.md#prevention-proposed-not-implemented).

## A task has a second PR that Conveyor doesn't track

Also seen on CV-9. After PR #90 merged, a comment reopened the task, and the agent opened #92 from a new branch (`cv/CV-9-readme-tone`). Conveyor follows one branch per repo (`cv/<KEY>-<slug>`), so it never saw #92 and the task won't finish when #92 merges.

**Fix.** Point the task at the PR it should follow:

```sh
conveyor task set KEY --repo <repo> --pr https://github.com/<owner>/<repo>/pull/92
conveyor task sync KEY
```

If that PR is already merged, the task can simply be closed: `conveyor task move KEY done`.

**Prevented since #95.** A comment on a task with a merged PR now starts a follow-up sub-task (`KEY` → `KEYA`) with its own branch, instead of reopening the task.
