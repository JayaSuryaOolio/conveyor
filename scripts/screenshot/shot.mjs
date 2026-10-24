// Screenshot the Conveyor frontend with mocked Wails bindings (no Go backend needed).
// Usage: node shot.mjs <out.png> [clickText] [mock.json]
//   clickText  text to click before shooting (default: the mock task title)
//   mock.json  per-binding overrides, e.g. {"Sessions":[...]}; merged over the defaults below
// Env: URL (default http://localhost:5173/), CHROME (browser executable), W/H (viewport)
// Setup: see scripts/screenshot/README.md
import {chromium} from 'playwright'
import {readFileSync} from 'fs'

const [out, clickText = 'UI changes', mockFile] = process.argv.slice(2)
if (!out) { console.error('usage: node shot.mjs <out.png> [clickText] [mock.json]'); process.exit(1) }
const overrides = mockFile ? JSON.parse(readFileSync(mockFile, 'utf8')) : {}

const b = await chromium.launch({executablePath: process.env.CHROME || undefined})
const p = await b.newPage({viewport: {width: +process.env.W || 1400, height: +process.env.H || 900}})
await p.addInitScript(overrides => {
  const now = new Date().toISOString()
  const repos = [{task_id: 1, repo_id: 1, repo_name: 'Conveyor', is_primary: true, branch: 'cv/CV-12-ui-changes', worktree_path: '/Users/me/.conveyor/workspaces/CV-12/Conveyor'}]
  const task = {id: 1, key: 'CV-12', title: 'UI changes', description: 'Move Shell and app icons out of Repos.', status: 'review', priority: 2, source: 'local', consecutive_failures: 0, created_at: now, updated_at: now, repos}
  const act = (label, kind, repo = 'Conveyor') => ({label, repo, cwd: '/x', cmd: '', kind})
  const data = {
    ListTasks: [{key: 'CV-12', title: 'UI changes', status: 'review', priority: 2, parent: '', repos: ['Conveyor'], lastSummary: '', ctxPeak: 0, updatedAt: now}],
    GetTask: {task, comments: [], runs: [], relations: {children: [], links: [], blocked_by_all: [], blocks_all: [], ancestors: [], descendants: []}},
    Actions: [act('Attach to session', 'attach'), act('New session from brief', 'fresh'), act('Shell in Conveyor', 'shell')],
    Sessions: [], AppIcons: {},
    ListApps: [{id: 1, name: 'VS Code', command: 'code', global: false, output: false}, {id: 2, name: 'Terminal', command: 'open', global: false, output: false}, {id: 3, name: 'Git status', command: 'git status', global: false, output: true}],
    DispatcherState: 'running',
    ...overrides,
  }
  // Any unlisted List*/Learn*/Agents*/Term* binding returns [], everything else null.
  const fn = n => () => (console.log('CALL ' + n), Promise.resolve(n in data ? data[n] : /^(List|Learn|Agents|Term)/.test(n) ? [] : null))
  window.go = {ui: {App: new Proxy({}, {get: (_, n) => fn(n)})}}
  window.runtime = new Proxy({}, {get: () => () => () => {}})
}, overrides)
p.on('console', m => m.text().startsWith('CALL') && console.log(m.text()))
p.on('pageerror', e => console.log('ERR', e.message))
await p.goto(process.env.URL || 'http://localhost:5173/')
if (clickText) await p.getByText(clickText).first().click()
await p.waitForTimeout(800)
await p.screenshot({path: out})
await b.close()
console.log('saved', out)
