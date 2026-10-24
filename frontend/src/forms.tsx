import {useState} from 'react'
import {AddAgent, AddRepo, CreateTask, GetSettings, ListAgents, ListRepos, SetSetting, SetTaskAgent} from '../wailsjs/go/ui/App'
import {core} from '../wailsjs/go/models'
import {Nav, errMsg} from './components'
import {useData} from './util'

export function NewTask({nav, close}: {nav: Nav; close: () => void}) {
    const {data: repos} = useData(ListRepos, [])
    const [title, setTitle] = useState('')
    const [desc, setDesc] = useState('')
    const [sel, setSel] = useState<string[]>([])
    const [primary, setPrimary] = useState('')
    const [model, setModel] = useState('')
    const [agent, setAgent] = useState('')
    const [start, setStart] = useState(false)
    const toggle = (n: string) => setSel(s => (s.includes(n) ? s.filter(x => x !== n) : [...s, n]))
    const submit = () =>
        CreateTask(core.NewTaskInput.createFrom({
            Title: title, Description: desc, Repos: sel, Primary: sel.includes(primary) ? primary : sel[0] ?? '',
            Start: start, Model: model, Agent: agent,
        })).then(key => { close(); nav({name: 'task', key}) }, errMsg)
    return (
        <div className="overlay" onClick={close}>
            <div className="dialog" onClick={e => e.stopPropagation()} onKeyDown={e => e.key === 'Escape' && close()}>
                <h3>New task</h3>
                <input autoFocus placeholder="Title" value={title} onChange={e => setTitle(e.target.value)}/>
                <textarea placeholder="Description" rows={5} value={desc} onChange={e => setDesc(e.target.value)}/>
                <div>Repos {repos?.length === 0 && <span className="muted">(add one on the Repos page first)</span>}</div>
                {repos?.map(r => (
                    <label key={r.id} className="row">
                        <input type="checkbox" checked={sel.includes(r.name)} onChange={() => toggle(r.name)}/> {r.name}
                        {sel.includes(r.name) && (
                            <label className="right"><input type="radio" name="primary" checked={(sel.includes(primary) ? primary : sel[0]) === r.name}
                                                            onChange={() => setPrimary(r.name)}/> primary</label>
                        )}
                    </label>
                ))}
                <input placeholder="Model (optional)" value={model} onChange={e => setModel(e.target.value)}/>
                <AgentSelect repos={sel} value={agent} onChange={setAgent}/>
                <label><input type="checkbox" checked={start} onChange={e => setStart(e.target.checked)}/> Start now (todo)</label>
                <div className="row"><button onClick={submit} disabled={!title.trim() || sel.length === 0}>Create</button><button onClick={close}>Cancel</button></div>
            </div>
        </div>
    )
}

/** Select of the Claude agents available to the given repos; "" means none. */
export function AgentSelect({repos, value, onChange}: {repos: string[]; value: string; onChange: (a: string) => void}) {
    const {data} = useData(() => ListAgents(repos), [repos.join(',')])
    const names = [...new Set((data ?? []).map(a => a.name))]
    if (value && !names.includes(value)) names.push(value)
    return (
        <select value={value} onChange={e => onChange(e.target.value)} title="Claude agent">
            <option value="">Agent: none</option>
            {names.map(n => <option key={n} value={n}>{n}</option>)}
        </select>
    )
}

export function TaskAgent({k, repos, agent}: {k: string; repos: string[]; agent: string}) {
    return <AgentSelect repos={repos} value={agent} onChange={a => SetTaskAgent(k, a).catch(errMsg)}/>
}

function Agents({repos}: {repos: string[]}) {
    const {data, error, reload} = useData(() => ListAgents(repos), [repos.join(',')])
    const [name, setName] = useState('')
    const [desc, setDesc] = useState('')
    const [prompt, setPrompt] = useState('')
    const [scope, setScope] = useState('user')
    const [hint, setHint] = useState('')
    const add = () => AddAgent(name, desc, prompt, scope).then(() => {
        setHint(scope === 'user' ? '' : `Commit .claude/agents/${name}.md in ${scope}, or task worktrees will not see it.`)
        setName(''); setDesc(''); setPrompt(''); reload()
    }, errMsg)
    return (
        <div>
            <h2>Agents</h2>
            <p className="muted">Claude Code subagents from each repo's .claude/agents and ~/.claude/agents.</p>
            {error && <p className="err">{error}</p>}
            <table><tbody>
                {data?.map(a => <tr key={a.scope + a.name}><td><b>{a.name}</b></td><td>{a.scope}</td><td className="muted">{a.description}</td></tr>)}
            </tbody></table>
            {hint && <p className="muted">{hint}</p>}
            <input placeholder="name (a-z, 0-9, -)" value={name} onChange={e => setName(e.target.value)}/>
            <input placeholder="description" value={desc} onChange={e => setDesc(e.target.value)}/>
            <textarea placeholder="Prompt" rows={4} value={prompt} onChange={e => setPrompt(e.target.value)}/>
            <div className="row">
                <select value={scope} onChange={e => setScope(e.target.value)}>
                    <option value="user">user (~/.claude/agents)</option>
                    {repos.map(r => <option key={r} value={r}>{r}</option>)}
                </select>
                <button onClick={add} disabled={!name || !desc.trim()}>Add agent</button>
            </div>
        </div>
    )
}

export function Repos() {
    const {data, error, reload} = useData(ListRepos, [])
    const [path, setPath] = useState('')
    const [name, setName] = useState('')
    return (
        <div>
            <h2>Repos</h2>
            {error && <p className="err">{error}</p>}
            <table><tbody>
                {data?.map(r => <tr key={r.id}><td><b>{r.name}</b></td><td>{r.default_branch}</td><td className="muted">{r.path}</td></tr>)}
            </tbody></table>
            <div className="row">
                <input placeholder="/path/to/clone" value={path} onChange={e => setPath(e.target.value)}/>
                <input placeholder="name (optional)" value={name} onChange={e => setName(e.target.value)}/>
                <button onClick={() => AddRepo(path, name).then(() => { setPath(''); setName(''); reload() }, errMsg)}>Add</button>
            </div>
            <Agents repos={data?.map(r => r.name) ?? []}/>
        </div>
    )
}

export function Settings() {
    const {data, error, reload} = useData(GetSettings, [])
    return (
        <div>
            <h2>Settings</h2>
            {error && <p className="err">{error}</p>}
            {data && Object.keys(data).sort().map(k => <SettingRow key={k} k={k} v={data[k]} reload={reload}/>)}
        </div>
    )
}

function SettingRow({k, v, reload}: {k: string; v: string; reload: () => void}) {
    const [val, setVal] = useState(v)
    return (
        <div className="row setting">
            <label>{k}</label>
            <input value={val} onChange={e => setVal(e.target.value)}/>
            <button disabled={val === v} onClick={() => SetSetting(k, val).then(reload, errMsg)}>Save</button>
        </div>
    )
}
