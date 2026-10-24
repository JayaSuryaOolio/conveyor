import {useEffect, useState} from 'react'
import {ActionIcon, Badge, Button, Code, Group, Modal, Paper, Stack, Table, Text, TextInput, Title} from '@mantine/core'
import {IconPencil, IconPlayerPlay, IconPlus, IconTrash} from '@tabler/icons-react'
import {AddApp, AppPresets, Confirm, DeleteApp, Home, ListApps, RenderApp, TestApp, UpdateApp, UserHooks} from '../wailsjs/go/ui/App'
import {store} from '../wailsjs/go/models'
import {errMsg} from './components'
import {useData} from './util'

function AppForm({initial, onClose}: {initial: store.App; onClose: (saved: boolean) => void}) {
    const [f, setF] = useState(initial)
    const [preview, setPreview] = useState('')
    const {data: home} = useData(Home, [])
    const sample = (home ?? '~/.conveyor') + '/workspaces/CV-1/my repo'
    useEffect(() => { RenderApp(f.command, sample).then(setPreview, errMsg) }, [f.command, sample])
    const ready = f.name.trim() && f.command.trim()
    const save = () => (f.id ? UpdateApp(f) : AddApp(f)).then(() => onClose(true), errMsg)
    return (
        <Stack gap="sm">
            <TextInput data-autofocus label="Name" value={f.name} onChange={e => setF({...f, name: e.target.value})}/>
            <TextInput label="Command" description="Run with sh -c. {cwd} becomes the quoted worktree path, e.g. open -a Zed {cwd}" ff="monospace"
                       value={f.command} onChange={e => setF({...f, command: e.target.value})}/>
            <div>
                <Text size="sm" fw={500}>Preview</Text>
                <Code block style={{whiteSpace: 'pre-wrap', wordBreak: 'break-all'}}>{preview}</Code>
            </div>
            <Group justify="space-between">
                <Button variant="default" leftSection={<IconPlayerPlay size={16}/>} disabled={!f.command.trim()} onClick={() => TestApp(f.command).catch(errMsg)}>Test on home folder</Button>
                <Group>
                    <Button variant="default" onClick={() => onClose(false)}>Cancel</Button>
                    <Button onClick={save} disabled={!ready}>{f.id ? 'Save' : 'Create'}</Button>
                </Group>
            </Group>
        </Stack>
    )
}

function AgentHooks() {
    const {data: status, error, reload} = useData(() => UserHooks('status'), [])
    const act = (a: string) => UserHooks(a).then(reload, errMsg)
    return (
        <Paper withBorder p="md" radius="md">
            <Stack gap="xs">
                <Group justify="space-between">
                    <Group gap="xs"><Title order={5}>Claude Code hooks</Title>
                        {status && <Badge variant="light" color={status === 'installed' ? 'green' : status === 'outdated' ? 'orange' : 'gray'}>{status}</Badge>}
                    </Group>
                    <Group gap="xs">
                        {status !== 'installed' && <Button size="xs" onClick={() => act('install')}>{status === 'outdated' ? 'Update' : 'Install'}</Button>}
                        {status && status !== 'not installed' && <Button size="xs" variant="default" onClick={() => act('uninstall')}>Uninstall</Button>}
                    </Group>
                </Group>
                <Text size="sm" c="dimmed">
                    Sessions Conveyor starts always report back. Installing adds the same hooks (prompt, stop, notification) to <Code>~/.claude/settings.json</Code>,
                    so a <Code>claude</Code> you start yourself in a task worktree, from an app or any terminal, shows up on the task as an interactive run.
                    Sessions outside Conveyor workspaces are ignored. Same as <Code>conveyor hooks install</Code>.
                </Text>
                {error && <Text c="red" size="sm">{error}</Text>}
            </Stack>
        </Paper>
    )
}

export function Apps() {
    const {data, error, reload} = useData(ListApps, [])
    const {data: presets} = useData(AppPresets, [])
    const [form, setForm] = useState<store.App>()
    const have = new Set((data ?? []).map(a => a.name))
    const missing = (presets ?? []).filter(p => !have.has(p.name))
    return (
        <Stack maw={900}>
            <Group justify="space-between">
                <Title order={3}>Apps</Title>
                <Button leftSection={<IconPlus size={16}/>} onClick={() => setForm(store.App.createFrom({id: 0, name: '', command: ''}))}>New app</Button>
            </Group>
            <Text size="sm" c="dimmed">Apps a task worktree can be opened in, from "Open in…" on the task page or <Code>conveyor app open NAME KEY</Code>.</Text>
            {error && <Text c="red">{error}</Text>}
            {data && data.length === 0 && <Text c="dimmed">None yet. Add one below or create your own.</Text>}
            {data && data.length > 0 && (
                <Table verticalSpacing="xs">
                    <Table.Thead><Table.Tr><Table.Th>Name</Table.Th><Table.Th>Command</Table.Th><Table.Th/></Table.Tr></Table.Thead>
                    <Table.Tbody>
                        {data.map(a => (
                            <Table.Tr key={a.id}>
                                <Table.Td fw={600}>{a.name}</Table.Td>
                                <Table.Td><Code>{a.command}</Code></Table.Td>
                                <Table.Td>
                                    <Group gap="xs" wrap="nowrap" justify="flex-end">
                                        <ActionIcon variant="subtle" color="gray" aria-label="Test" title="Test on home folder" onClick={() => TestApp(a.command).catch(errMsg)}><IconPlayerPlay size={16}/></ActionIcon>
                                        <ActionIcon variant="subtle" color="gray" aria-label="Edit" onClick={() => setForm(a)}><IconPencil size={16}/></ActionIcon>
                                        <ActionIcon variant="subtle" color="red" aria-label="Delete" onClick={() => Confirm(`Delete app ${a.name}?`).then(ok => { if (ok) return DeleteApp(a.name).then(reload) }).catch(errMsg)}><IconTrash size={16}/></ActionIcon>
                                    </Group>
                                </Table.Td>
                            </Table.Tr>
                        ))}
                    </Table.Tbody>
                </Table>
            )}
            {missing.length > 0 && <>
                <Title order={5}>Detected on this Mac</Title>
                <Group gap="xs">
                    {missing.map(p => (
                        <Button key={p.name} size="xs" variant="light" leftSection={<IconPlus size={14}/>} title={p.command}
                                onClick={() => AddApp(p).then(reload, errMsg)}>{p.name}</Button>
                    ))}
                </Group>
            </>}
            <AgentHooks/>
            <Modal opened={!!form} onClose={() => setForm(undefined)} title={form?.id ? 'Edit app' : 'New app'} size="lg">
                {form && <AppForm initial={form} onClose={saved => { setForm(undefined); if (saved) reload() }}/>}
            </Modal>
        </Stack>
    )
}
