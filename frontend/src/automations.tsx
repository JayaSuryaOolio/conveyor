import {ListAutomations, RunAutomation, SetAutomationEnabled} from '../wailsjs/go/ui/App'
import {errMsg} from './components'
import {useData} from './util'

export function Automations() {
    const {data, error, reload} = useData(ListAutomations, [])
    return (
        <div>
            <h2>Automations</h2>
            {error && <p className="err">{error}</p>}
            {data && data.length === 0 && <p className="muted">None yet. Add one with <code>conveyor automation add --template pr-sweep</code>.</p>}
            {data && data.length > 0 && (
                <table>
                    <thead><tr><th>Enabled</th><th>Name</th><th>Schedule</th><th>Kind</th><th>Last run</th><th>Result</th><th/></tr></thead>
                    <tbody>
                        {data.map(a => (
                            <tr key={a.name}>
                                <td><input type="checkbox" checked={a.enabled} onChange={e => SetAutomationEnabled(a.name, e.target.checked).then(reload, errMsg)}/></td>
                                <td><b>{a.name}</b></td>
                                <td><code>{a.schedule}</code></td>
                                <td>{a.action_kind}</td>
                                <td className="muted">{a.last_run_at ?? ''}</td>
                                <td className="clamp">{a.last_result ?? ''}</td>
                                <td><button onClick={() => RunAutomation(a.name).then(reload, e => { errMsg(e); reload() })}>Run now</button></td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            )}
        </div>
    )
}
