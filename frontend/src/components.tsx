import {useState} from 'react'
import {AddComment, Move, ShowError} from '../wailsjs/go/ui/App'
import {ui} from '../wailsjs/go/models'
import {ALL_STATUSES, STATUS_LABEL, fmtK} from './util'

export type Nav = (v: {name: string; key?: string}) => void

export const errMsg = (e: unknown) => ShowError(String(e))

export function StatusSelect({k, status}: {k: string; status: string}) {
    return (
        <select value={status} onClick={e => e.stopPropagation()}
                onChange={e => Move(k, e.target.value).catch(errMsg)}>
            {ALL_STATUSES.map(s => <option key={s} value={s}>{STATUS_LABEL[s]}</option>)}
        </select>
    )
}

export function Card({c, nav}: {c: ui.TaskCard; nav: Nav}) {
    return (
        <div className="card" onClick={() => nav({name: 'task', key: c.key})}>
            <div className="row"><b>{c.key}</b>
                {c.ctxPeak > 0 && <span className="badge" title="context peak">{fmtK(c.ctxPeak)}</span>}
            </div>
            <div>{c.title}</div>
            <div className="chips">{c.repos.map(r => <span key={r} className="chip">{r}</span>)}</div>
            {c.lastSummary && <div className="muted clamp">{c.lastSummary}</div>}
            <StatusSelect k={c.key} status={c.status}/>
        </div>
    )
}

export function CommentBox({k, placeholder = 'Comment…', onDone}: {k: string; placeholder?: string; onDone?: () => void}) {
    const [text, setText] = useState('')
    const send = () => {
        if (!text.trim()) return
        AddComment(k, text).then(() => { setText(''); onDone?.() }, errMsg)
    }
    return (
        <div className="commentbox">
            <textarea value={text} placeholder={placeholder} rows={2} onChange={e => setText(e.target.value)}
                      onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) send() }}/>
            <button onClick={send}>Send</button>
        </div>
    )
}
