export PATH=$PATH:$HOME/go/bin
cd /Users/jayasurya/work/conveyor-wt/011
gofmt -l .; go vet ./... && go test ./... 2>&1 | tail -20
cd frontend && npm ci >/dev/null 2>&1 && npm run build 2>&1 | tail -8
cd .. && wails build 2>&1 | tail -4
git checkout -- frontend/dist/.gitkeep; git status --short
