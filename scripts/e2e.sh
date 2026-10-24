#!/usr/bin/env bash
# End-to-end smoke test: real `claude` (haiku) on two throwaway repos.
# Repos must be trusted by Claude Code (worktrees inherit trust from the main
# repo, a trusted parent folder does not count). Create them under E2E_ROOT
# (default ~/work/conveyor-e2e) and run `claude` once in each by hand:
#   cd ~/work/conveyor-e2e/repo-a && claude   # accept the trust prompt, then exit
# Re-runs reuse those paths, so trust persists.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
E2E_ROOT="${E2E_ROOT:-$HOME/work/conveyor-e2e}"
export CONVEYOR_HOME="$E2E_ROOT/home"
BIN="$E2E_ROOT/conveyor"
TIMEOUT="${E2E_TIMEOUT:-180}"
DAEMON_PID=""

fail() { echo "FAIL: $*" >&2; exit 1; }
cv() { "$BIN" "$@"; }

cleanup() {
  [ -n "$DAEMON_PID" ] && kill "$DAEMON_PID" 2>/dev/null && wait "$DAEMON_PID" 2>/dev/null || true
  if [ -f "$CONVEYOR_HOME/conveyor.db" ]; then
    for b in $(cv task list --json 2>/dev/null | jq -r '.[].key' | while read -r k; do
      cv task show "$k" --runs --json | jq -r '.runs[]?.bg_id // empty'; done); do
      claude rm "$b" >/dev/null 2>&1 || true
    done
  fi
  # keep the (trusted) repo dirs; remove only what this run created
  rm -rf "$CONVEYOR_HOME" "$E2E_ROOT/origin-a.git" "$E2E_ROOT/origin-b.git" "$BIN"
}
trap cleanup EXIT

mkdir -p "$E2E_ROOT"
rm -rf "$CONVEYOR_HOME" "$E2E_ROOT"/origin-*.git
(cd "$ROOT_DIR" && go build -o "$BIN" ./cmd/conveyor)

for r in a b; do
  git init -q --bare "$E2E_ROOT/origin-$r.git"
  d="$E2E_ROOT/repo-$r"
  if [ ! -d "$d/.git" ]; then
    git init -q -b main "$d"
    git -C "$d" commit -q --allow-empty -m init
  fi
  git -C "$d" remote remove origin 2>/dev/null || true
  git -C "$d" remote add origin "$E2E_ROOT/origin-$r.git"
  git -C "$d" push -q -u origin main
  git -C "$d" remote set-head origin main >/dev/null
  cv repo add "$d" --name "repo-$r"
done

out=$(cv task new "E2E hello" --repo repo-a,repo-b --todo --model haiku \
  --desc "Create hello.txt with 'hi' in each repo, commit on the task branch, do not push. End with the status block.")
echo "$out"
KEY=${out%% *}

cv daemon >"$E2E_ROOT/daemon.log" 2>&1 &
DAEMON_PID=$!

# wait_status <status> <min runs>
wait_review() {
  local want_runs=$1 end=$((SECONDS + TIMEOUT)) st n
  while [ $SECONDS -lt $end ]; do
    kill -0 "$DAEMON_PID" 2>/dev/null || { cat "$E2E_ROOT/daemon.log" >&2; fail "daemon exited (untrusted repo? run claude once in $E2E_ROOT/repo-a and repo-b)"; }
    j=$(cv task show "$KEY" --runs --json)
    st=$(jq -r .status <<<"$j"); n=$(jq '.runs|length' <<<"$j")
    case $st in failed|needs_you) echo "$j" >&2; fail "task reached $st";; esac
    [ "$st" = review ] && [ "$n" -ge "$want_runs" ] && return 0
    sleep 3
  done
  cat "$E2E_ROOT/daemon.log" >&2
  fail "timeout waiting for review (run $want_runs)"
}

wait_review 1
j=$(cv task show "$KEY" --runs --thread --json)
for p in $(jq -r '.repos[].worktree_path' <<<"$j"); do
  [ -n "$(git -C "$p" log --format=%H main..HEAD)" ] || fail "no commit in $p"
done
sid=$(jq -r '.runs[0].session_id // empty' <<<"$j")
[ -n "$sid" ] || fail "run 1 has no session_id"
[ "$(jq '.runs[0].ctx_peak' <<<"$j")" -gt 0 ] || fail "ctx_peak is 0"
[ "$(jq '[.comments[]|select(.author=="agent")]|length' <<<"$j")" -ge 1 ] || fail "no agent comment"
echo "run 1 ok (session $sid)"

cv task comment "$KEY" "Also add bye.txt"
sleep 5 # let the comment move the task out of review
wait_review 2
j=$(cv task show "$KEY" --runs --json)
[ "$(jq -r '.runs[1].mode' <<<"$j")" = resume ] || fail "run 2 mode is not resume"
[ "$(jq -r '.runs[1].session_id' <<<"$j")" = "$sid" ] || fail "run 2 session_id differs"
echo "PASS"
