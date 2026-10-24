import {useState} from 'react'
import {Button, Checkbox, Group, Modal, MultiSelect, Select, Stack, Table, Text, TextInput, Textarea, Title} from '@mantine/core'
import {AddRepo, CreateTask, GetSettings, ListAgents, ListRepos, SetSetting, SetTaskAgent} from '../wailsjs/go/ui/App'
import {core} from '../wailsjs/go/models'
import {Nav, errMsg} from './components'
import {MarkdownEditor} from './markdown'
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
    const prim = sel.includes(primary) ? primary : sel[0] ?? ''
    const submit = () =>
        CreateTask(core.NewTaskInput.createFrom({
            Title: title, Description: desc, Repos: sel, Primary: prim,
            Start: start, Model: model, Agent: agent,
        })).then(key => { close(); nav({name: 'task', key}) }, errMsg)
    return (
        <Modal opened onClose={close} title="New task" size="md">
            <Stack gap="sm">
                <TextInput data-autofocus placeholder="Title" value={title} onChange={e => setTitle(e.target.value)}/>
                <MarkdownEditor minRows={4} placeholder="Description" value={desc} onChange={setDesc}/>
                <MultiSelect label="Repos" placeholder="Pick repos" description={repos?.length === 0 ? 'Add one on the Repos page first' : undefined}
                             data={repos?.map(r => r.name) ?? []} value={sel} onChange={setSel}/>
                {sel.length > 1 && <Select label="Primary repo" allowDeselect={false} data={sel} value={prim} onChange={v => setPrimary(v ?? '')}/>}
                <TextInput placeholder="Model (optional)" value={model} onChange={e => setModel(e.target.value)}/>
                <AgentSelect repos={sel} value={agent} onChange={setAgent}/>
                <Checkbox label="Start now (todo)" checked={start} onChange={e => setStart(e.currentTarget.checked)}/>
                <Group justify="flex-end">
                    <Button variant="default" onClick={close}>Cancel</Button>
                    <Button onClick={submit} disabled={!title.trim() || sel.length === 0}>Create</Button>
                </Group>
            </Stack>
        </Modal>
    )
}

/** Select of the Claude agents available to the given repos; "" means none. */
export function AgentSelect({repos, value, onChange}: {repos: string[]; value: string; onChange: (a: string) => void}) {
    const {data} = useData(() => ListAgents(repos), [repos.join(',')])
    const names = [...new Set((data ?? []).map(a => a.name))]
    if (value && !names.includes(value)) names.push(value)
    return <Select placeholder="Agent: none" aria-label="Claude agent" clearable data={names} value={value || null} onChange={v => onChange(v ?? '')}/>
}

export function TaskAgent({k, repos, agent}: {k: string; repos: string[]; agent: string}) {
    return <AgentSelect repos={repos} value={agent} onChange={a => SetTaskAgent(k, a).catch(errMsg)}/>
}

export function Repos() {
    const {data, error, reload} = useData(ListRepos, [])
    const [path, setPath] = useState('')
    const [name, setName] = useState('')
    return (
        <Stack maw={900}>
            <Title order={3}>Repos</Title>
            {error && <Text c="red">{error}</Text>}
            <Table>
                <Table.Tbody>
                    {data?.map(r => <Table.Tr key={r.id}><Table.Td fw={600}>{r.name}</Table.Td><Table.Td>{r.default_branch}</Table.Td><Table.Td c="dimmed">{r.path}</Table.Td></Table.Tr>)}
                </Table.Tbody>
            </Table>
            <Group align="flex-start" wrap="nowrap">
                <TextInput style={{flex: 2}} placeholder="/path/to/clone" value={path} onChange={e => setPath(e.target.value)}/>
                <TextInput style={{flex: 1}} placeholder="name (optional)" value={name} onChange={e => setName(e.target.value)}/>
                <Button onClick={() => AddRepo(path, name).then(() => { setPath(''); setName(''); reload() }, errMsg)}>Add</Button>
            </Group>
        </Stack>
    )
}

export function Settings() {
    const {data, error, reload} = useData(GetSettings, [])
    return (
        <Stack maw={700}>
            <Title order={3}>Settings</Title>
            {error && <Text c="red">{error}</Text>}
            {data && Object.keys(data).sort().map(k => <SettingRow key={k} k={k} v={data[k]} reload={reload}/>)}
        </Stack>
    )
}

function SettingRow({k, v, reload}: {k: string; v: string; reload: () => void}) {
    const [val, setVal] = useState(v)
    return (
        <Group wrap="nowrap" align="flex-end">
            <TextInput style={{flex: 1}} label={k} value={val} onChange={e => setVal(e.target.value)}/>
            <Button disabled={val === v} onClick={() => SetSetting(k, val).then(reload, errMsg)}>Save</Button>
        </Group>
    )
}
