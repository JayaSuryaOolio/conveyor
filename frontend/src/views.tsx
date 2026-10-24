import {Fragment, useEffect, useRef, useState} from 'react'
import {Cancel, Confirm, GetTask, ListTasks, RunLog} from '../wailsjs/go/ui/App'
import {core, store} from '../wailsjs/go/models'
import {Card, CommentBox, Nav, errMsg} from './components'
import {TaskAgent} from './forms'
import {TaskSidebar} from './workspace'
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

const secs = (n: number) => (n < 60 ? n + 's' : Math.floor(n / 60) + 'm ' + (n % 60) + 's')

/** Chat log of a run's Claude session: polls while the run is active, once on demand after. */
function SessionLog({run}: {run: store.Run}) {
    const active = run.status === 'launching' || run.status === 'running'
    const [open, setOpen] = useState(false)
    const [entries, setEntries] = useState<core.RunLog['entries']>([])
    const [err, setErr] = useState('')
    const [now, setNow] = useState(Date.now())
    const after = useRef(0)
    const box = useRef<HTMLDivElement>(null)
    const show = active || open
    useEffect(() => {
        if (!show) return
        let stop = false
        const poll = () => RunLog(run.id, after.current).then(l => {
            if (stop) return
            after.current = l.next
            setErr('')
            if (l.entries.length) setEntries(e => [...e, ...l.entries])
        }, e => { if (!stop) setErr(String(e)) })
        poll()
        if (!active) return () => { stop = true }
        const p = setInterval(poll, 2000), c = setInterval(() => setNow(Date.now()), 1000)
        return () => { stop = true; clearInterval(p); clearInterval(c) }
    }, [run.id, show, active])
    useEffect(() => { box.current?.scrollTo({top: box.current.scrollHeight}) }, [entries])
    const lastTool = [...entries].reverse().find(e => e.kind === 'tool')
    return (
        <div className="session">
            {active ? (
                <div className="working">
                    <span className="pulse"/>
                    <span>Working · {secs(Math.max(0, Math.round((now - Date.parse(run.started_at)) / 1000)))}</span>
                    {lastTool && <span className="muted clamp">· {lastTool.text}</span>}
                </div>
            ) : (
                <a href="#" onClick={e => { e.preventDefault(); setOpen(!open) }}>{open ? 'hide session' : 'show session'}</a>
            )}
            {err && <div className="muted">{err}</div>}
            {show && entries.length > 0 && (
                <div className="chatlog" ref={box}>
                    {entries.map((e, i) => <div key={i} className={'log-' + e.kind}>{e.text}</div>)}
                </div>
            )}
        </div>
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
                    <TaskAgent k={t.key} repos={t.repos?.map(r => r.repo_name) ?? []} agent={t.agent ?? ''}/>
                    <button onClick={() => Confirm('Cancel this task?').then(ok => { if (ok) return Cancel(t.key) }).catch(errMsg)}>Cancel task</button>
                </div>
                <pre className="desc">{t.description || '(no description)'}</pre>
                {latest?.criteria && <><h4>Acceptance criteria</h4><Criteria text={latest.criteria}/></>}
                <h4>Thread</h4>
                {items.map(it => it.run ? (
                    <Fragment key={'r' + it.run.id}>
                        <div className="runsep">
                            Run {runNo.get(it.run.id)} · {it.run.mode} · {it.run.api_calls} calls · {fmtK(it.run.ctx_peak)} peak · {fmtDuration(it.run.started_at, it.run.ended_at)}
                        </div>
                        <SessionLog run={it.run}/>
                    </Fragment>
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
                <TaskSidebar t={t} runs={d.runs} reload={reload}/>
            </div>
        </div>
    )
}
