import {useCallback, useEffect, useRef, useState} from 'react'
import {EventsOn} from '../wailsjs/runtime/runtime'
import {ListTasks} from '../wailsjs/go/ui/App'

// One db:changed listener for the whole app. Wails calls listeners newest first, so a separate
// cache-invalidating listener could run after the hooks that refetch; bumping dbVersion here first avoids that.
let dbVersion = 0
let subs: Set<() => void> | undefined
function dbSubs() {
    if (!subs) {
        const s = subs = new Set()
        EventsOn('db:changed', () => { dbVersion++; s.forEach(f => f()) })
    }
    return subs
}

/** Calls f after every DB change; returns the unsubscribe. */
export function onDbChanged(f: () => void): () => void {
    const s = dbSubs()
    s.add(f)
    return () => { s.delete(f) }
}

/** Wraps fn so every caller between two DB changes shares one call (and its in-flight promise). */
export function perDbVersion<T>(fn: () => Promise<T>): () => Promise<T> {
    let cached: {v: number; p: Promise<T>} | undefined
    return () => {
        dbSubs()
        if (cached?.v !== dbVersion) {
            const c = cached = {v: dbVersion, p: fn()}
            c.p.catch(() => { if (cached === c) cached = undefined }) // retry on the next call
        }
        return cached.p
    }
}

/** ListTasks, fetched once per DB change however many components show it. */
export const listTasks = perDbVersion(ListTasks)

/** Loads data with fn and reloads when deps change and, unless static, when the DB changes. Responses to superseded loads are dropped. */
export function useData<T>(fn: () => Promise<T>, deps: unknown[], opts?: {static?: boolean}): {data?: T; error?: string; reload: () => void} {
    const [data, setData] = useState<T>()
    const [error, setError] = useState<string>()
    const seq = useRef(0)
    const reload = useCallback(() => {
        const n = ++seq.current
        fn().then(d => { if (n === seq.current) { setData(d); setError(undefined) } },
            e => { if (n === seq.current) setError(String(e)) })
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, deps)
    useEffect(() => { reload(); return opts?.static ? undefined : onDbChanged(reload) }, [reload]) // eslint-disable-line react-hooks/exhaustive-deps
    return {data, error, reload}
}

/** useState backed by localStorage under `draft:<key>`, so half-typed text survives navigating away. Empty clears it. The setter also takes an updater, like useState's. */
export function useDraft(key: string): [string, (v: string | ((prev: string) => string)) => void] {
    const read = () => { try { return localStorage.getItem('draft:' + key) ?? '' } catch { return '' } }
    const [val, setVal] = useState(read)
    const latest = useRef(val) // so updaters see the newest value even when calls are batched
    useEffect(() => { latest.current = read(); setVal(latest.current) }, [key]) // eslint-disable-line react-hooks/exhaustive-deps
    const set = (v: string | ((prev: string) => string)) => {
        const next = typeof v === 'function' ? v(latest.current) : v
        latest.current = next
        setVal(next)
        try { next ? localStorage.setItem('draft:' + key, next) : localStorage.removeItem('draft:' + key) } catch { /* private mode */ }
    }
    return [val, set]
}

export const fmtK = (n: number) => (n >= 1000 ? Math.round(n / 1000) + 'K' : String(n))

export function fmtDuration(start: string, end?: string | null): string {
    if (!end) return 'running'
    const s = Math.max(0, Math.round((Date.parse(end) - Date.parse(start)) / 1000))
    return s < 60 ? s + 's' : s < 3600 ? Math.round(s / 60) + 'm' : (s / 3600).toFixed(1) + 'h'
}

export const STATUS_LABEL: Record<string, string> = {
    backlog: 'Backlog', todo: 'Todo', running: 'Running', needs_you: 'Needs you',
    review: 'Review', done: 'Done', failed: 'Failed', cancelled: 'Cancelled',
}
export const ALL_STATUSES = Object.keys(STATUS_LABEL)

export const STATUS_COLOR: Record<string, string> = {
    backlog: 'gray', todo: 'blue', running: 'cyan', needs_you: 'orange',
    review: 'violet', done: 'green', failed: 'red', cancelled: 'gray',
}

/** "5m ago" style relative time; pair with fmtTime for the absolute one. */
export function ago(iso: string): string {
    const s = Math.round((Date.now() - Date.parse(iso)) / 1000)
    if (!(s >= 60)) return 'just now'
    if (s < 3600) return Math.floor(s / 60) + 'm ago'
    if (s < 86400) return Math.floor(s / 3600) + 'h ago'
    return Math.floor(s / 86400) + 'd ago'
}

export const fmtTime = (iso: string) => new Date(iso).toLocaleString()
