# 012: End-to-end smoke test with real Claude, README

**Wave 5** · depends on: all · branch `task/012-e2e`

## Goal
Prove the whole loop with the real `claude` CLI on throwaway repos, fix what breaks, and document how to use Conveyor.

## Do
1. **`scripts/e2e.sh`**
   - Uses a temp `CONVEYOR_HOME` and two temp git repos with a bare origin.
   - **Trust caveat:** the e2e repos must be trusted (spike-bg.md). The script prints instructions if the launch fails with `ErrUntrusted`. In CI or agent runs, create the repos under a path the operator already trusts, e.g. `~/work/conveyor-e2e/`, and run `claude` there once by hand. Note this in the README.
   - Builds the CLI, then:
     1. Adds the repos.
     2. Creates a two-repo task with `--todo` and `--model haiku`: "Create hello.txt with 'hi' in each repo, commit on the task branch, do not push. End with the status block."
     3. Runs `conveyor daemon` in the background.
     4. Waits up to 3 min for `review`.
     5. Asserts that both worktrees have the commit, the run has a `session_id`, `ctx_peak > 0`, and an agent comment exists.
     6. Posts a comment ("Also add bye.txt"), waits for `review` again, and asserts the second run's `mode=resume` and the same `session_id`.
     7. Stops the daemon, runs `claude rm` on the sessions, and removes the temp dirs.
2. Fix bugs found in any package, with a small regression test each.
3. **`README.md`**: install (`go install ./cmd/conveyor`, `wails build`), the first-run steps (add a repo, create a task, start the app or the daemon), the CLI reference (generated from the registry's help strings), integrations (terminal template, automations), and the trust caveat.
4. `progress/progress.md`: final state.

## Done when
`scripts/e2e.sh` passes locally against real Claude (haiku), and `go test ./...` and `wails build` pass.
