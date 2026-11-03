import type {Location} from 'react-router'
import type {View} from './components'

// Views <-> URLs. The app still speaks `nav(View)`; react-router (hash history: works under Wails' custom
// scheme and vite dev without server fallback) owns history/back. Only this file knows the URL shape.
//   /board?label=epic&repo=x   /task/KEY   /epic/KEY   /epic/KEY/task/KEY   /agents ...
// refactor: if routes grow params, move to <Routes> with element per path and drop View.

export function toPath(v: View): string {
    if (v.name === 'epic' && v.key) return `/epic/${v.key}` + (v.task ? `/task/${v.task}` : '')
    if (v.name === 'task' && v.key) return `/task/${v.key}`
    const q = new URLSearchParams()
    if (v.label) q.set('label', v.label)
    if (v.repo) q.set('repo', v.repo)
    return `/${v.name}` + (q.size ? `?${q}` : '')
}

export function fromLocation({pathname, search}: Location): View {
    const [name = 'board', key, sub, task] = pathname.split('/').filter(Boolean).map(decodeURIComponent)
    if (name === 'task' && key) return {name, key}
    if (name === 'epic' && key) return {name, key, task: sub === 'task' ? task : undefined}
    const p = new URLSearchParams(search)
    return {name, label: p.get('label') ?? undefined, repo: p.get('repo') ?? undefined}
}
