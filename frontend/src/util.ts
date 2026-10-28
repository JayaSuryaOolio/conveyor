import {useCallback, useEffect, useRef, useState} from 'react'
import {EventsOn} from '../wailsjs/runtime/runtime'

/** Loads data with fn and reloads when the DB changes or deps change. */
export function useData<T>(fn: () => Promise<T>, deps: unknown[]): {data?: T; error?: string; reload: () => void} {
    const [data, setData] = useState<T>()
    const [error, setError] = useState<string>()
    const reload = useCallback(() => {
        fn().then(d => { setData(d); setError(undefined) }, e => setError(String(e)))
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, deps)
    useEffect(() => { reload(); return EventsOn('db:changed', reload) }, [reload])
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
