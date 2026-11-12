import {useEffect, useState} from 'react'
import {GetSettings, ListAgents, ListRepos} from '../wailsjs/go/ui/App'
import {listTasks, onDbChanged, perDbVersion} from './util'

export type Raw = {
    tasks: {key: string; title: string; status: string}[]
    agents: {name: string; scope: string; description: string}[]
    titles: Record<string, string> // every task incl. done/cancelled, for task key tooltips in rendered markdown
    labels: string[]
    repos: string[]
    keyPrefix: string // the key_prefix setting with its dash, e.g. "CV-"
}

async function fetchRaw(): Promise<Raw> {
    const repos = (await ListRepos()).map(r => r.name)
    const [tasks, agents, settings] = await Promise.all([listTasks(), ListAgents(repos), GetSettings()])
    return {
        tasks: tasks.filter(t => t.status !== 'done' && t.status !== 'cancelled').map(t => ({key: t.key, title: t.title, status: t.status})),
        agents: agents.map(a => ({name: a.name, scope: a.scope, description: a.description})),
        titles: Object.fromEntries(tasks.map(t => [t.key, t.title])),
        labels: [...new Set(tasks.flatMap(t => t.labels))].sort(),
        repos: repos.sort(),
        keyPrefix: settings.key_prefix + '-',
    }
}

let cached = perDbVersion(fetchRaw)

/**
 * Board items and agents, fetched at most once per DB change (or on the palette's reload button, which
 * also picks up agent files, since those live outside the DB); shared by the Cmd+K palette, the editor's
 * task key autocomplete and task-key tooltips so none of them hits the DB per keystroke.
 */
export function load(force: boolean): Promise<Raw> {
    if (force) cached = perDbVersion(fetchRaw)
    return cached()
}

/** The task key prefix with its dash ("CV-"); '' until loaded. Follows key_prefix setting changes. */
export function useKeyPrefix(): string {
    const [p, setP] = useState('')
    useEffect(() => {
        const get = () => load(false).then(r => setP(r.keyPrefix), () => {})
        get()
        return onDbChanged(get)
    }, [])
    return p
}

/** A regex source matching s literally. */
export const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
