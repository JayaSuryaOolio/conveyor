import {useState} from 'react'
import {ActionIcon, Select, Table, TextInput, Textarea, Title} from '@mantine/core'
import {Button, Group, Modal, Stack, Text} from './ui'
import {IconCopy, IconPencil, IconPlus, IconRefresh, IconSearch} from '@tabler/icons-react'
import {AddAgent, ListAgents, ListRepos, ReadAgent, SaveAgent, SetAgentModel} from '../wailsjs/go/ui/App'
import {errMsg} from './components'
import {ModelSelect} from './forms'
import {useData} from './util'

export function Agents() {
    const {data: repoList} = useData(ListRepos, [])
    const repos = repoList?.map(r => r.name) ?? []
    const {data, error, reload} = useData(() => ListAgents(repos), [repos.join(',')])
    const [filter, setFilter] = useState('all')
    const [q, setQ] = useState('')
    const [edit, setEdit] = useState<{path: string; text: string} | null>(null)
    const [open, setOpen] = useState(false)
    const [name, setName] = useState('')
    const [desc, setDesc] = useState('')
    const [prompt, setPrompt] = useState('')
    const [scope, setScope] = useState('user')
    const [model, setModel] = useState('')
    const [hint, setHint] = useState('')
    const add = () => AddAgent(name, desc, prompt, scope, model).then(() => {
        setHint(scope === 'user' ? '' : `Commit .claude/agents/${name}.md in ${scope}, or task worktrees will not see it.`)
        setName(''); setDesc(''); setPrompt(''); setModel(''); setOpen(false); reload()
    }, errMsg)
    const needle = q.trim().toLowerCase()
    const shown = data?.filter(a => (filter === 'all' || a.scope === filter) &&
        (!needle || [a.name, a.description, a.scope].some(v => v.toLowerCase().includes(needle))))
    const startEdit = (path: string) => ReadAgent(path).then(text => setEdit({path, text}), errMsg)
    const save = () => edit && SaveAgent(edit.path, edit.text).then(() => { setEdit(null); reload() }, errMsg)
    return (
        <Stack style={{maxWidth: 900}}>
            <Group justify="between">
                <Title order={3}>Agents</Title>
                <Group>
                    <TextInput aria-label="Search agents" placeholder="Search" leftSection={<IconSearch size={14}/>} value={q} onChange={e => setQ(e.target.value)}/>
                    <ActionIcon variant="subtle" color="gray" aria-label="Refresh agents" title="Reload agent files from disk" onClick={reload}><IconRefresh size={18}/></ActionIcon>
                    <Select aria-label="Scope" allowDeselect={false} value={filter} onChange={v => setFilter(v ?? 'all')}
                            data={[{value: 'all', label: 'All scopes'}, {value: 'user', label: 'user'}, ...repos.map(r => ({value: r, label: r}))]}/>
                    <Button icon={<IconPlus size={16}/>} onClick={() => setOpen(true)}>Add agent</Button>
                </Group>
            </Group>
            <Text size="sm" tone="muted">Claude Code subagents from each repo's .claude/agents and ~/.claude/agents.</Text>
            {error && <Text tone="danger">{error}</Text>}
            {hint && <Text size="sm" tone="muted">{hint}</Text>}
            <Table>
                <Table.Thead>
                    <Table.Tr><Table.Th miw={160}>Name</Table.Th><Table.Th miw={160}>Scope</Table.Th><Table.Th miw={160}>Model</Table.Th><Table.Th>Description</Table.Th><Table.Th w={40}/></Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                    {shown?.map(a => (
                        <Table.Tr key={a.scope + a.name}>
                            <Table.Td fw={600} style={{whiteSpace: 'nowrap'}}>{a.name}</Table.Td>
                            <Table.Td style={{whiteSpace: 'nowrap'}}><Text size="sm" tone={a.scope === 'user' ? 'muted' : 'info'}>{a.scope}</Text></Table.Td>
                            <Table.Td>
                                <ModelSelect size="xs" aria-label={`Model of ${a.name}`} value={a.model} onChange={m => SetAgentModel(a.scope, a.name, m).then(reload, errMsg)}/>
                            </Table.Td>
                            <Table.Td><Text size="sm" tone="muted" clamp={2} title={a.description}>{a.description}</Text></Table.Td>
                            <Table.Td>
                                {a.scope === 'user'
                                    ? <ActionIcon variant="subtle" color="gray" aria-label={`Edit ${a.name}`} title="Edit file" onClick={() => startEdit(a.path)}><IconPencil size={16}/></ActionIcon>
                                    : <ActionIcon variant="subtle" color="gray" aria-label={`Copy path of ${a.name}`} title="Copy file path" onClick={() => navigator.clipboard.writeText(a.path).catch(errMsg)}><IconCopy size={16}/></ActionIcon>}
                            </Table.Td>
                        </Table.Tr>
                    ))}
                </Table.Tbody>
            </Table>
            <Modal opened={!!edit} onClose={() => setEdit(null)} title="Edit agent file" size="lg">
                <Stack gap="sm">
                    <Text size="sm" tone="muted" mono>{edit?.path}</Text>
                    <Textarea autosize minRows={10} maxRows={24} styles={{input: {fontFamily: 'monospace'}}} value={edit?.text ?? ''} onChange={e => setEdit(v => v && {...v, text: e.target.value})}/>
                    <Group justify="end">
                        <Button variant="secondary" onClick={() => setEdit(null)}>Cancel</Button>
                        <Button onClick={save}>Save</Button>
                    </Group>
                </Stack>
            </Modal>
            <Modal opened={open} onClose={() => setOpen(false)} title="Add agent" size="md">
                <Stack gap="sm">
                    <TextInput data-autofocus placeholder="name (a-z, 0-9, -)" value={name} onChange={e => setName(e.target.value)}/>
                    <TextInput placeholder="description" value={desc} onChange={e => setDesc(e.target.value)}/>
                    <Textarea autosize minRows={4} maxRows={14} placeholder="Prompt" value={prompt} onChange={e => setPrompt(e.target.value)}/>
                    <Select label="Scope" allowDeselect={false} value={scope} onChange={v => setScope(v ?? 'user')}
                            data={[{value: 'user', label: 'user (~/.claude/agents)'}, ...repos.map(r => ({value: r, label: r}))]}/>
                    <ModelSelect label="Model" value={model} onChange={setModel}/>
                    <Group justify="end">
                        <Button variant="secondary" onClick={() => setOpen(false)}>Cancel</Button>
                        <Button onClick={add} disabled={!name || !desc.trim()}>Add agent</Button>
                    </Group>
                </Stack>
            </Modal>
        </Stack>
    )
}
