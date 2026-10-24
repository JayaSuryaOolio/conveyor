import {Fragment} from 'react'
import {Cancel, Confirm, GetTask, ListTasks} from '../wailsjs/go/ui/App'
import {store} from '../wailsjs/go/models'
import {Card, CommentBox, Nav, StatusSelect, errMsg} from './components'
import {TaskAgent} from './forms'
import {WorkspacePanel} from './workspace'
import {fmtDuration, fmtK, useData} from './util'

const COLUMNS = [['backlog', 'Backlog'], ['todo', 'Todo'], ['running', 'Running'], ['needs_you', 'Needs you'], ['review', 'Review'], ['done', 'Done (7d)']]
const FOCUS_ORDER = ['needs_you', 'review', 'failed', 'running', 'todo']
const WEEK = 7 * 24 * 3600 * 1000

export function Board({nav, q}: {nav: Nav; q: string}) {
    const {data, error} = useData(ListTasks, [])
    if (error) return <p className="err">{error}</p>
    const needle = q.toLowerCase()
    const cards = (data ?? []).filter(c => !needle || (c.key + ' ' + c.title).toLowerCase().includes(needle))
    const cols = [...COLUMNS]
    if (cards.some(c => c.status === 'failed')) cols.splice(5, 0, ['failed', 'Failed'])
    return (
        <div className="board">
            {cols.map(([s, label]) => {
                let list = cards.filter(c => c.status === s)
                if (s === 'done') list = list.filter(c => Date.now() - Date.parse(c.updatedAt) < WEEK)
                return (
                    <section key={s} className="col">
                        <h3>{label} <span className="muted">{list.length}</span></h3>
                        {list.map(c => <Card key={c.key} c={c} nav={nav}/>)}
                    </section>
                )
            })}
        </div>
    )
}

export function Focus({nav, q}: {nav: Nav; q: string}) {
    const {data, error, reload} = useData(ListTasks, [])
    if (error) return <p className="err">{error}</p>
    const needle = q.toLowerCase()
    const rows = (data ?? [])
        .filter(c => FOCUS_ORDER.includes(c.status) && (!needle || (c.key + ' ' + c.title).toLowerCase().includes(needle)))
        .sort((a, b) => FOCUS_ORDER.indexOf(a.status) - FOCUS_ORDER.indexOf(b.status))
    return (
        <div className="focus">
            {rows.length === 0 && <p className="muted">Nothing needs you.</p>}
            {rows.map(c => (
                <div key={c.key} className="focusrow">
                    <div className="row">
                        <span className={'status s-' + c.status}>{c.status.replace('_', ' ')}</span>
                        <b>{c.key}</b> <span>{c.title}</span>
                        <button className="right" onClick={() => nav({name: 'task', key: c.key})}>Open</button>
                    </div>
                    {c.lastSummary && <div className="muted clamp">{c.lastSummary}</div>}
                    <CommentBox k={c.key} onDone={reload}/>
                </div>
            ))}
        </div>
    )
}

type Item = {t: string; run?: store.Run; comment?: store.Comment}

function Criteria({text}: {text: string}) {
    const lines = text.split('\n').map(l => l.trim()).filter(l => /^[-*]\s*\[[ xX]\]/.test(l))
    if (!lines.length) return null
    return (
        <ul className="criteria">
            {lines.map((l, i) => {
                const done = /\[[xX]\]/.test(l)
                return <li key={i}>{done ? '☑' : '☐'} {l.replace(/^[-*]\s*\[[ xX]\]\s*/, '')}</li>
            })}
        </ul>
    )
}

export function TaskPage({k, nav}: {k: string; nav: Nav}) {
    const {data: d, error, reload} = useData(() => GetTask(k), [k])
    if (error) return <p className="err">{error}</p>
    if (!d) return <p className="muted">Loading…</p>
    const t = d.task
    const items: Item[] = [
        ...d.runs.map((run): Item => ({t: run.started_at, run})),
        ...d.comments.map((comment): Item => ({t: comment.created_at, comment})),
    ].sort((a, b) => (a.t < b.t ? -1 : a.t > b.t ? 1 : a.run ? -1 : 1))
    const runNo = new Map(d.runs.map((r, i) => [r.id, i + 1]))
    const latest = d.runs[d.runs.length - 1]
    return (
        <div className="task">
            <div className="main">
                <p><a href="#" onClick={e => { e.preventDefault(); nav({name: 'board'}) }}>← Board</a></p>
                <h2>{t.key} · {t.title}</h2>
                <div className="row">
                    <StatusSelect k={t.key} status={t.status}/>
                    <TaskAgent k={t.key} repos={t.repos?.map(r => r.repo_name) ?? []} agent={t.agent ?? ''}/>
                    <button onClick={() => Confirm('Cancel this task?').then(ok => { if (ok) return Cancel(t.key) }).catch(errMsg)}>Cancel task</button>
                    {t.repos?.map(r => <span key={r.repo_id} className="chip">{r.repo_name}{r.is_primary ? ' ★' : ''}</span>)}
                </div>
                <pre className="desc">{t.description || '(no description)'}</pre>
                {latest?.criteria && <><h4>Acceptance criteria</h4><Criteria text={latest.criteria}/></>}
                <h4>Thread</h4>
                {items.map(it => it.run ? (
                    <div key={'r' + it.run.id} className="runsep">
                        Run {runNo.get(it.run.id)} · {it.run.mode} · {it.run.api_calls} calls · {fmtK(it.run.ctx_peak)} peak · {fmtDuration(it.run.started_at, it.run.ended_at)}
                    </div>
                ) : (
                    <Fragment key={'c' + it.comment!.id}>
                        <div className={'comment by-' + it.comment!.author}>
                            <div className="muted">{it.comment!.author} · {it.comment!.created_at}</div>
                            <pre>{it.comment!.body}</pre>
                        </div>
                    </Fragment>
                ))}
                <CommentBox k={t.key} onDone={reload}/>
            </div>
            <div className="side">
                <WorkspacePanel k={t.key} repos={t.repos ?? []} runs={d.runs} reload={reload}/>
            </div>
        </div>
    )
}
