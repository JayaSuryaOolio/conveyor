import {ListAgents, ListRepos, ListTasks} from '../wailsjs/go/ui/App'

const CACHE_KEY = 'palette-cache-v2'
const TTL = 24 * 3600 * 1000
export type Raw = {
    tasks: {key: string; title: string; status: string}[]
    agents: {name: string; scope: string; description: string}[]
    labels: string[]
    repos: string[]
}

let mem: {at: number; raw: Raw} | undefined // skips the localStorage JSON.parse on repeated reads (e.g. editor keystrokes)

/**
 * Board items and agents are fetched once a day (or on the palette's reload button); shared by the
 * Cmd+K palette and the editor's CV- autocomplete so neither hits the DB per keystroke.
 * Refactor: the cache is stale for up to a day, so a task created since won't autocomplete until reload;
 * a Wails event on task create/delete could invalidate it.
 */
export async function load(force: boolean): Promise<Raw> {
    if (!force && mem && Date.now() - mem.at < TTL) return mem.raw
    try {
        const c = JSON.parse(localStorage.getItem(CACHE_KEY) ?? 'null')
        if (!force && c && Date.now() - c.at < TTL) { mem = c; return c.raw }
    } catch {}
    const repos = (await ListRepos()).map(r => r.name)
    const [tasks, agents] = await Promise.all([ListTasks(), ListAgents(repos)])
    const raw: Raw = {
        tasks: tasks.filter(t => t.status !== 'done' && t.status !== 'cancelled').map(t => ({key: t.key, title: t.title, status: t.status})),
        agents: agents.map(a => ({name: a.name, scope: a.scope, description: a.description})),
        labels: [...new Set(tasks.flatMap(t => t.labels))].sort(),
        repos: repos.sort(),
    }
    mem = {at: Date.now(), raw}
    try { localStorage.setItem(CACHE_KEY, JSON.stringify(mem)) } catch {}
    return raw
}
