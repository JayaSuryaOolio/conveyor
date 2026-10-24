import {Actions, EnsureWorkspace, RunAction, Sessions} from '../wailsjs/go/ui/App'
import {core, store} from '../wailsjs/go/models'
import {errMsg} from './components'
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

export function WorkspacePanel({k, repos, runs}: {k: string; repos: store.TaskRepo[]; runs: store.Run[]}) {
    const {data: actions} = useData(() => Actions(k), [k])
    const {data: sessions} = useData(() => Sessions(k), [k])
    if (!repos.some(r => r.worktree_path)) {
        return (
            <aside className="workspace">
                <h4>Workspace</h4>
                <p className="muted">Workspace not created yet.</p>
                <button onClick={() => EnsureWorkspace(k).catch(errMsg)}>Create workspace</button>
            </aside>
        )
    }
    const taskActions = (actions ?? []).filter(a => ['attach', 'resume', 'fresh'].includes(a.kind))
    return (
        <aside className="workspace">
            <h4>Workspace</h4>
            {repos.map(r => (
                <div key={r.repo_id} className="wsrepo">
                    <b>{r.repo_name}</b> <span className="muted">{r.branch}</span>
                    {r.worktree_path && <div className="muted path" title="Click to copy" onClick={() => copy(r.worktree_path!)}>{r.worktree_path}</div>}
                    {r.pr_url && <div><a href={r.pr_url} target="_blank" rel="noreferrer">PR</a> <span className="muted">{r.pr_state}</span></div>}
                    <div className="actions">
                        {(actions ?? []).filter(a => a.repo === r.repo_name && ['shell', 'editor'].includes(a.kind)).map(a => <ActionButton key={a.kind} k={k} a={a}/>)}
                    </div>
                </div>
            ))}
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
