import {useState} from 'react'
import {Actions, EnsureWorkspace, RunAction, Sessions, SetTaskRepo} from '../wailsjs/go/ui/App'
import {core, store} from '../wailsjs/go/models'
import {BrowserOpenURL} from '../wailsjs/runtime/runtime'
import {StatusSelect, errMsg} from './components'
import {useData} from './util'

const copy = (s: string) => navigator.clipboard.writeText(s).catch(errMsg)
const sq = (s: string) => "'" + s.replace(/'/g, "'\\''") + "'"
const cdCmd = (a: core.Action) => 'cd ' + sq(a.cwd) + (a.cmd && a.kind !== 'editor' ? ' && ' + a.cmd : '')

function ActionButton({k, a, kind}: {k: string; a: core.Action; kind?: string}) {
    return (
        <span className="actionbtn">
            <button onClick={() => RunAction(k, kind ?? a.kind, a.repo).catch(errMsg)}>{a.label}</button>
            <button title="Copy command" onClick={() => copy(a.kind === 'editor' ? a.cmd : cdCmd(a))}>⧉</button>
        </span>
    )
}

// RepoFields shows one repo's branch, worktree and PR. The branch is editable
// until the worktree exists; the PR URL always is.
function RepoFields({k, r, reload, children}: {k: string; r: store.TaskRepo; reload: () => void; children?: React.ReactNode}) {
    const [branch, setBranch] = useState(r.branch)
    const [pr, setPr] = useState(r.pr_url ?? '')
    const dirty = branch !== r.branch || pr !== (r.pr_url ?? '')
    const save = () => SetTaskRepo(k, r.repo_name, branch, pr).then(reload).catch(errMsg)
    const onKey = (e: React.KeyboardEvent) => { if (e.key === 'Enter' && dirty) save() }
    return (
        <div className="wsrepo">
            <b>{r.repo_name}</b>{r.is_primary && <span className="chip" title="Primary repo"> ★ primary</span>}
            <label className="field">Branch
                {r.worktree_path
                    ? <div className="muted path">{r.branch}</div>
                    : <input value={branch} onChange={e => setBranch(e.target.value)} onKeyDown={onKey}/>}
            </label>
            <div className="field">Worktree
                {r.worktree_path
                    ? <div className="row"><span className="muted path">{r.worktree_path}</span><button title="Copy path" onClick={() => copy(r.worktree_path!)}>⧉</button></div>
                    : <div className="muted">not created yet</div>}
            </div>
            <label className="field">PR
                <input value={pr} placeholder="https://github.com/…/pull/1" onChange={e => setPr(e.target.value)} onKeyDown={onKey}/>
            </label>
            <div className="row">
                {r.pr_url && <a href={r.pr_url} onClick={e => { e.preventDefault(); BrowserOpenURL(r.pr_url!) }}>Open PR</a>}
                {r.pr_url && r.pr_state && <span className="chip">{r.pr_state}</span>}
                {dirty && <button className="right" onClick={save}>Save</button>}
            </div>
            <div className="actions">{children}</div>
        </div>
    )
}

export function TaskSidebar({t, runs, reload}: {t: store.Task; runs: store.Run[]; reload: () => void}) {
    const k = t.key
    const repos = t.repos ?? []
    const {data: actions, reload: reloadActions} = useData(() => Actions(k), [k])
    const {data: sessions, reload: reloadSessions} = useData(() => Sessions(k), [k])
    const [busy, setBusy] = useState(false)
    const taskActions = (actions ?? []).filter(a => ['attach', 'resume', 'fresh'].includes(a.kind))
    const hasWorktree = repos.some(r => r.worktree_path)
    return (
        <aside className="workspace">
            <div className="row"><b>Status</b><StatusSelect k={k} status={t.status}/></div>
            <div className="muted">Created {t.created_at}</div>
            <div className="muted">Updated {t.updated_at}</div>
            <h4>Repos</h4>
            {repos.map(r => (
                <RepoFields key={r.repo_id + '|' + r.branch + '|' + (r.pr_url ?? '')} k={k} r={r} reload={reload}>
                    {(actions ?? []).filter(a => a.repo === r.repo_name && ['shell', 'editor'].includes(a.kind)).map(a => <ActionButton key={a.kind} k={k} a={a}/>)}
                </RepoFields>
            ))}
            {!hasWorktree && (
                <button disabled={busy} onClick={() => { setBusy(true); EnsureWorkspace(k).then(() => { reload(); reloadActions(); reloadSessions() }).catch(errMsg).finally(() => setBusy(false)) }}>{busy ? 'Creating…' : 'Create workspace'}</button>
            )}
            <div className="actions">{taskActions.map(a => <ActionButton key={a.kind} k={k} a={a}/>)}</div>
            <h4>Sessions</h4>
            {runs.length + (sessions?.length ?? 0) === 0 && <p className="muted">None yet.</p>}
            {runs.map((r, i) => (
                <div key={r.id} className="muted">#{i + 1} {r.kind} · {r.status}{r.block_status ? ' · ' + r.block_status : ''}</div>
            ))}
            {sessions?.map(s => (
                <div key={s.id} className="session">
                    <ActionButton k={k} a={s.action} kind={'resume:' + s.id}/>
                    <div className="muted clamp" title={s.prompt}>{s.prompt || s.id}</div>
                </div>
            ))}
        </aside>
    )
}
