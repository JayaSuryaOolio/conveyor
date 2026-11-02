import {useState} from 'react'
import {Button, Group, Modal, Select, Stack, Table, Text, TextInput, Textarea, Title} from '@mantine/core'
import {IconPlus} from '@tabler/icons-react'
import {AddAgent, ListAgents, ListRepos, SetAgentModel} from '../wailsjs/go/ui/App'
import {errMsg} from './components'
import {ModelSelect} from './forms'
import {useData} from './util'

export function Agents() {
    const {data: repoList} = useData(ListRepos, [])
    const repos = repoList?.map(r => r.name) ?? []
    const {data, error, reload} = useData(() => ListAgents(repos), [repos.join(',')])
    const [filter, setFilter] = useState('all')
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
    const shown = data?.filter(a => filter === 'all' || a.scope === filter)
    return (
        <Stack maw={900}>
            <Group justify="space-between">
                <Title order={3}>Agents</Title>
                <Group>
                    <Select aria-label="Scope" allowDeselect={false} value={filter} onChange={v => setFilter(v ?? 'all')}
                            data={[{value: 'all', label: 'All scopes'}, {value: 'user', label: 'user'}, ...repos.map(r => ({value: r, label: r}))]}/>
                    <Button leftSection={<IconPlus size={16}/>} onClick={() => setOpen(true)}>Add agent</Button>
                </Group>
            </Group>
            <Text size="sm" c="dimmed">Claude Code subagents from each repo's .claude/agents and ~/.claude/agents.</Text>
            {error && <Text c="red">{error}</Text>}
            {hint && <Text size="sm" c="dimmed">{hint}</Text>}
            <Table>
                <Table.Thead>
                    <Table.Tr><Table.Th miw={160}>Name</Table.Th><Table.Th miw={160}>Scope</Table.Th><Table.Th miw={160}>Model</Table.Th><Table.Th>Description</Table.Th></Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                    {shown?.map(a => (
                        <Table.Tr key={a.scope + a.name}>
                            <Table.Td fw={600} style={{whiteSpace: 'nowrap'}}>{a.name}</Table.Td>
                            <Table.Td style={{whiteSpace: 'nowrap'}}><Text size="sm" c={a.scope === 'user' ? 'dimmed' : 'blue'}>{a.scope}</Text></Table.Td>
                            <Table.Td>
                                <ModelSelect size="xs" aria-label={`Model of ${a.name}`} value={a.model} onChange={m => SetAgentModel(a.scope, a.name, m).then(reload, errMsg)}/>
                            </Table.Td>
                            <Table.Td><Text size="sm" c="dimmed" lineClamp={2} title={a.description}>{a.description}</Text></Table.Td>
                        </Table.Tr>
                    ))}
                </Table.Tbody>
            </Table>
            <Modal opened={open} onClose={() => setOpen(false)} title="Add agent" size="md">
                <Stack gap="sm">
                    <TextInput data-autofocus placeholder="name (a-z, 0-9, -)" value={name} onChange={e => setName(e.target.value)}/>
                    <TextInput placeholder="description" value={desc} onChange={e => setDesc(e.target.value)}/>
                    <Textarea autosize minRows={4} maxRows={14} placeholder="Prompt" value={prompt} onChange={e => setPrompt(e.target.value)}/>
                    <Select label="Scope" allowDeselect={false} value={scope} onChange={v => setScope(v ?? 'user')}
                            data={[{value: 'user', label: 'user (~/.claude/agents)'}, ...repos.map(r => ({value: r, label: r}))]}/>
                    <ModelSelect label="Model" value={model} onChange={setModel}/>
                    <Group justify="flex-end">
                        <Button variant="default" onClick={() => setOpen(false)}>Cancel</Button>
                        <Button onClick={add} disabled={!name || !desc.trim()}>Add agent</Button>
                    </Group>
                </Stack>
            </Modal>
        </Stack>
    )
}
