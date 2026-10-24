# Screenshot script

Screenshots the frontend in a browser with the Wails `window.go.ui.App` / `window.runtime` bindings mocked, so no Go backend is needed. Handy for checking UI changes and attaching images to PR descriptions.

```sh
# once
(cd scripts/screenshot && npm install && npx playwright install chromium)
# per shot
(cd frontend && npm run dev -- --port 5173 &)   # vite dev server
node scripts/screenshot/shot.mjs /tmp/out.png            # click the default mock task, shoot
node scripts/screenshot/shot.mjs /tmp/out.png "" mock.json  # no click, override bindings
```

Env: `URL`, `CHROME` (browser path), `W`/`H` (viewport). Edit the mock data in `shot.mjs` for new states; every binding call is logged as `CALL <name>` so you can see which ones to mock.
