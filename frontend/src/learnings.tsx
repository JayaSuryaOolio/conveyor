import {useState} from 'react'
import {AcceptLearning, ApplyLearnings, EditLearning, Learnings as ListLearnings, RejectLearning} from '../wailsjs/go/ui/App'
import {ui} from '../wailsjs/go/models'
import {Nav, errMsg} from './components'
import {useData} from './util'

export function LearningsPage({nav}: {nav: Nav}) {
    const {data, error, reload} = useData(ListLearnings, [])
    const groups: Record<string, ui.LearningRow[]> = {}
    data?.forEach(l => (groups[l.repo || '(personal)'] ||= []).push(l))
    return (
        <div>
            <h2>Learnings</h2>
            {error && <p className="err">{error}</p>}
            {data?.length === 0 && <p className="muted">Nothing proposed.</p>}
            {Object.keys(groups).sort().map(repo => (
                <section key={repo}>
                    <div className="row"><h3>{repo}</h3>
                        {repo !== '(personal)' && groups[repo].some(l => l.status === 'accepted') && (
                            <button onClick={() => ApplyLearnings(repo).then(k => k && nav({name: 'task', key: k}), errMsg)}>Apply to {repo}</button>
                        )}
                    </div>
                    {groups[repo].map(l => <LearningItem key={l.id} l={l} reload={reload}/>)}
                </section>
            ))}
        </div>
    )
}

function LearningItem({l, reload}: {l: ui.LearningRow; reload: () => void}) {
    const [body, setBody] = useState(l.body)
    const [target, setTarget] = useState(l.target ?? 'CLAUDE.md')
    const done = () => reload()
    return (
        <div className="row">
            <input style={{flex: 1}} value={body} onChange={e => setBody(e.target.value)}/>
            <button disabled={body === l.body} onClick={() => EditLearning(l.id, body).then(done, errMsg)}>Save</button>
            <input style={{width: 120}} title="target file" value={target} onChange={e => setTarget(e.target.value)}/>
            <button disabled={l.status === 'accepted' && target === l.target} onClick={() => AcceptLearning(l.id, target).then(done, errMsg)}>
                {l.status === 'accepted' ? 'Accepted' : 'Accept'}
            </button>
            <button onClick={() => RejectLearning(l.id).then(done, errMsg)}>Reject</button>
        </div>
    )
}
