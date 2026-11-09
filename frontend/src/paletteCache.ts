import {ListAgents, ListRepos} from '../wailsjs/go/ui/App'
import {listTasks, perDbVersion} from './util'

export type Raw = {
    tasks: {key: string; title: string; status: string}[]
    agents: {name: string; scope: string; description: string}[]
    titles: Record<string, string> // every task incl. done/cancelled, for CV- key tooltips in rendered markdown
    labels: string[]
    repos: string[]
}

async function fetchRaw(): Promise<Raw> {
    const repos = (await ListRepos()).map(r => r.name)
    const [tasks, agents] = await Promise.all([listTasks(), ListAgents(repos)])
    return {
        tasks: tasks.filter(t => t.status !== 'done' && t.status !== 'cancelled').map(t => ({key: t.key, title: t.title, status: t.status})),
        agents: agents.map(a => ({name: a.name, scope: a.scope, description: a.description})),
        titles: Object.fromEntries(tasks.map(t => [t.key, t.title])),
        labels: [...new Set(tasks.flatMap(t => t.labels))].sort(),
        repos: repos.sort(),
    }
}

let cached = perDbVersion(fetchRaw)

/**
 * Board items and agents, fetched at most once per DB change (or on the palette's reload button, which
 * also picks up agent files, since those live outside the DB); shared by the Cmd+K palette, the editor's
 * CV- autocomplete and task-key tooltips so none of them hits the DB per keystroke.
 */
export function load(force: boolean): Promise<Raw> {
    if (force) cached = perDbVersion(fetchRaw)
    return cached()
}
