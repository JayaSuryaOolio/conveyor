#!/usr/bin/env bash
# Installs the conveyor CLI (go install) and the Conveyor desktop app.
# APP_DIR overrides where Conveyor.app goes (default /Applications).
set -euo pipefail

cd "$(dirname "$0")/.."
APP_DIR=${APP_DIR:-/Applications}
user_path=$PATH
export PATH="$PATH:$(go env GOPATH)/bin"

for c in go git npm claude; do
	command -v "$c" >/dev/null || { echo "missing: $c" >&2; exit 1; }
done
command -v gh >/dev/null || echo "warning: gh not found; PR tracking will not work" >&2
command -v wails >/dev/null || go install "github.com/wailsapp/wails/v2/cmd/wails@$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v2)"

echo "==> CLI"
go install ./cmd/conveyor
bin="$(go env GOBIN)"
bin="${bin:-$(go env GOPATH)/bin}/conveyor"

echo "==> App"
wails build -clean
git checkout -- frontend/dist/.gitkeep 2>/dev/null || true
rm -rf "$APP_DIR/Conveyor.app"
cp -R build/bin/Conveyor.app "$APP_DIR/"

echo "installed: $bin"
echo "installed: $APP_DIR/Conveyor.app"
case ":$user_path:" in
*":$(dirname "$bin"):"*) ;;
*) echo "note: add $(dirname "$bin") to your PATH to use 'conveyor' from a shell" ;;
esac
