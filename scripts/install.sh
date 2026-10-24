#!/usr/bin/env bash
# Installs the conveyor CLI and the Conveyor desktop app.
# BIN_DIR overrides where the CLI goes (default ~/.local/bin, where claude installs itself);
# APP_DIR overrides where Conveyor.app goes (default /Applications).
set -euo pipefail

cd "$(dirname "$0")/.."
BIN_DIR=${BIN_DIR:-$HOME/.local/bin}
APP_DIR=${APP_DIR:-/Applications}
user_path=$PATH
export PATH="$PATH:$(go env GOPATH)/bin"

for c in go git npm claude; do
	command -v "$c" >/dev/null || { echo "missing: $c" >&2; exit 1; }
done
command -v gh >/dev/null || echo "warning: gh not found; PR tracking will not work" >&2
command -v wails >/dev/null || go install "github.com/wailsapp/wails/v2/cmd/wails@$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v2)"

echo "==> CLI"
mkdir -p "$BIN_DIR"
GOBIN="$BIN_DIR" go install ./cmd/conveyor

echo "==> App"
wails build -clean
git checkout -- frontend/dist/.gitkeep 2>/dev/null || true
rm -rf "$APP_DIR/Conveyor.app"
cp -R build/bin/Conveyor.app "$APP_DIR/"

echo "==> Skills"
# Conveyor's user-level skills; overwritten on every install, so edit them in this repo.
mkdir -p "$HOME/.claude/skills"
for d in skills/*/; do
	rm -rf "$HOME/.claude/skills/$(basename "$d")"
	cp -R "$d" "$HOME/.claude/skills/"
	echo "installed: ~/.claude/skills/$(basename "$d")"
done

echo "==> Weekly doctor"
# A weekly task, on the repo registered for this checkout, that runs the conveyor-doctor skill.
repo=$("$BIN_DIR/conveyor" repo list --json | python3 -c 'import json,os,sys; p=os.path.realpath(sys.argv[1]); print(next((r["name"] for r in json.load(sys.stdin) or [] if os.path.realpath(r["path"])==p), ""))' "$PWD")
if [ -z "$repo" ]; then
	echo "skipped: register this checkout first (conveyor repo add $PWD), then re-run"
elif "$BIN_DIR/conveyor" automation list | grep -q '^conveyor-doctor '; then
	echo "exists: automation conveyor-doctor"
else
	"$BIN_DIR/conveyor" automation add conveyor-doctor --schedule "0 9 * * 1" --kind create_task \
		--arg "{\"title\":\"Conveyor doctor: weekly run review\",\"description\":\"Use the conveyor-doctor skill to review last week's runs and propose prompt and context fixes.\",\"repos\":[\"$repo\"],\"start\":true}"
fi

echo "installed: $BIN_DIR/conveyor"
echo "installed: $APP_DIR/Conveyor.app"
case ":$user_path:" in
*":$BIN_DIR:"*) ;;
*) echo "note: add $BIN_DIR to your PATH; the app and hooks find conveyor through it" ;;
esac
