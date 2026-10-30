import {useEffect, useRef, useState} from 'react'
import {ActionIcon, Badge, Button, Checkbox, Code, FileButton, Group, Modal, MultiSelect, Paper, Stack, Switch, Table, Tabs, Text, TextInput, Textarea, Title} from '@mantine/core'
import {IconDownload, IconPencil, IconPlayerPlay, IconPlus, IconPuzzle, IconTransform, IconTrash, IconUpload} from '@tabler/icons-react'
import {AddApp, GetSettings, SetSetting, AppIcons, AppPresets, Confirm, ConvertApp, DeleteApp, ExportApp, Home, InstallManifest, InstallPreset, ListApps, ManifestSchema, RenderApp, TestApp, UpdateApp, UserHooks, ValidateManifest} from '../wailsjs/go/ui/App'
import {store} from '../wailsjs/go/models'
import {AppIcon, errMsg, runApp, toast} from './components'
import {Field, SettingRow} from './forms'
import {useData} from './util'

function AppForm({initial, onClose}: {initial: store.App; onClose: (saved: boolean) => void}) {
    const [f, setF] = useState(initial)
    const [preview, setPreview] = useState('')
    const {data: home} = useData(Home, [])
    const sample = (home ?? '~/.conveyor') + '/workspaces/CV-1/my repo'
    useEffect(() => { RenderApp(f.command, sample, 'CV-1').then(setPreview, errMsg) }, [f.command, sample])
    const ready = f.name.trim() && f.command.trim()
    const save = () => (f.id ? UpdateApp(f) : AddApp(f)).then(() => onClose(true), errMsg)
    return (
        <Stack gap="sm">
            <TextInput data-autofocus label="Name" value={f.name} onChange={e => setF({...f, name: e.target.value})}/>
            <TextInput label="Command" description="Run with sh -c. {cwd} becomes the quoted worktree path and {key} the task key, e.g. open -a Zed {cwd}" ff="monospace"
                       value={f.command} onChange={e => setF({...f, command: e.target.value})}/>
            <Switch label="Global" description="Not tied to a worktree: shown in the left menu, not on task pages" checked={f.global}
                    onChange={e => setF({...f, global: e.currentTarget.checked})}/>
            <Switch label="Quick action" checked={f.output} onChange={e => setF({...f, output: e.currentTarget.checked})}
                    description={<>Runs to completion in the task's primary worktree and shows its output as a toast; a button in the task sidebar. Scripts can call <Code>conveyor task sessions {'{key}'}</Code> for the task's Claude session ids.</>}/>
            <div>
                <Text size="sm" fw={500}>Preview</Text>
                <Code block style={{whiteSpace: 'pre-wrap', wordBreak: 'break-all'}}>{preview}</Code>
            </div>
            <Group justify="space-between">
                <Button variant="default" leftSection={<IconPlayerPlay size={16}/>} disabled={!f.command.trim()} onClick={() => runApp(f, TestApp(f))}>Test on home folder</Button>
                <Group>
                    <Button variant="default" onClick={() => onClose(false)}>Cancel</Button>
                    <Button onClick={save} disabled={!ready}>{f.id ? 'Save' : 'Create'}</Button>
                </Group>
            </Group>
        </Stack>
    )
}

const MANIFEST_EXAMPLE = `{
  "version": 2,
  "name": "Lint",
  "description": "Lint the task's worktree",
  "actions": [
    {"id": "lint", "label": "Lint", "places": ["task_sidebar", "board_card"], "output": true,
     "run": "cd \${{ worktree.path }} && echo Linting \${{ task.key }} && make lint"}
  ]
}`

/** Build and install a manifest: JSON editor with insertable placeholders, place-aware "Add action", live validation. */
function ManifestForm({onClose}: {onClose: (saved: boolean) => void}) {
    const [text, setText] = useState('')
    const [places, setPlaces] = useState<string[]>(['task_sidebar'])
    const [problem, setProblem] = useState('')
    const [replace, setReplace] = useState(false)
    const ref = useRef<HTMLTextAreaElement>(null)
    const {data: schema} = useData(ManifestSchema, [])
    useEffect(() => {
        if (!text.trim()) { setProblem(''); return }
        const t = setTimeout(() => ValidateManifest(text).then(setProblem, errMsg), 250)
        return () => clearTimeout(t)
    }, [text])
    const insert = (s: string) => {
        const el = ref.current, [a, b] = el ? [el.selectionStart, el.selectionEnd] : [text.length, text.length]
        setText(text.slice(0, a) + s + text.slice(b))
        setTimeout(() => { el?.focus(); el?.setSelectionRange(a + s.length, a + s.length) })
    }
    const addAction = () => {
        let m: any
        try { m = text.trim() ? JSON.parse(text) : {version: Number(schema?.version?.[0] ?? 1), name: 'My app'} } catch { errMsg('Fix the JSON first: ' + problem); return }
        const n = (m.actions?.length ?? 0) + 1
        m.actions = [...(m.actions ?? []), {id: `action-${n}`, label: `Action ${n}`, places, run: 'echo hello'}]
        setText(JSON.stringify(m, null, 2))
    }
    return (
        <Stack gap="sm">
            <Text size="sm" c="dimmed">
                A manifest (version {schema?.version?.[0]}) declares actions and where they appear, and can ship <Code>agents</Code> and <Code>automations</Code>.
                Click a placeholder to insert it at the cursor; scripts get its value shell-quoted.
            </Text>
            <Group gap={6}>
                {(schema?.vars ?? []).map(v => <Button key={v} size="compact-xs" variant="light" ff="monospace" onClick={() => insert(`\${{ ${v} }}`)}>{v}</Button>)}
            </Group>
            <Group align="flex-end" gap="xs">
                <MultiSelect label="Places" size="xs" data={schema?.places ?? []} value={places} onChange={setPlaces} style={{flex: 1}}/>
                <Button size="xs" variant="default" leftSection={<IconPlus size={14}/>} disabled={!places.length} onClick={addAction}>Add action</Button>
            </Group>
            <Textarea ref={ref} autosize minRows={10} maxRows={20} ff="monospace" placeholder={MANIFEST_EXAMPLE} value={text} onChange={e => setText(e.target.value)}
                      error={problem || undefined}/>
            <Group justify="space-between">
                <FileButton accept="application/json,.json" onChange={f => f?.text().then(setText)}>
                    {p => <Button {...p} variant="default" leftSection={<IconUpload size={16}/>}>Load file</Button>}
                </FileButton>
                <Group>
                    <Checkbox label="Replace app with the same name" checked={replace} onChange={e => setReplace(e.currentTarget.checked)}/>
                    <Button variant="default" onClick={() => onClose(false)}>Cancel</Button>
                    <Button disabled={!text.trim() || !!problem} onClick={() => InstallManifest(text, replace).then(() => onClose(true), errMsg)}>Install</Button>
                </Group>
            </Group>
        </Stack>
    )
}

function manifestSummary(a: store.App) {
    try {
        const m = JSON.parse(a.manifest)
        return [`${m.actions?.length ?? 0} actions`, m.agents?.length && `${m.agents.length} agents`, m.automations?.length && `${m.automations.length} automations`].filter(Boolean).join(', ')
    } catch { return 'invalid manifest' }
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

function AppList() {
    const {data, error, reload} = useData(ListApps, [])
    const {data: presets} = useData(AppPresets, [])
    const {data: icons} = useData(AppIcons, [])
    const [form, setForm] = useState<store.App>()
    const [importing, setImporting] = useState(false)
    const have = new Set((data ?? []).map(a => a.name))
    const missing = (presets ?? []).filter(p => !have.has(p.name))
    return (
        <Stack maw={900}>
            <Group justify="space-between">
                <Title order={3}>Apps</Title>
                <Group gap="xs">
                    <Button variant="default" leftSection={<IconPuzzle size={16}/>} onClick={() => setImporting(true)}>Install manifest</Button>
                    <Button leftSection={<IconPlus size={16}/>} onClick={() => setForm(store.App.createFrom({id: 0, name: '', command: '', global: false, output: false, manifest: ''}))}>New app</Button>
                </Group>
            </Group>
            <Text size="sm" c="dimmed">Apps a task worktree can be opened in, from the icons under each repo on the task page or <Code>conveyor app open NAME KEY</Code>.
                Global apps (Slack, a dashboard, ...) are not tied to a worktree: they show as icons at the bottom of the left menu instead, and <Code>{'{cwd}'}</Code> is your home folder.</Text>
            {error && <Text c="red">{error}</Text>}
            {data && data.length === 0 && <Text c="dimmed">None yet. Add one below or create your own.</Text>}
            {data && data.length > 0 && (
                <Table verticalSpacing="xs">
                    <Table.Thead><Table.Tr><Table.Th>Name</Table.Th><Table.Th>Command</Table.Th><Table.Th>Global</Table.Th><Table.Th/></Table.Tr></Table.Thead>
                    <Table.Tbody>
                        {data.map(a => (
                            <Table.Tr key={a.id}>
                                <Table.Td fw={600}><Group gap="xs" wrap="nowrap"><AppIcon name={a.name} icons={icons}/>{a.name}{a.output && <Badge size="xs" variant="light">quick action</Badge>}{a.manifest && <Badge size="xs" variant="light" color="grape">manifest</Badge>}</Group></Table.Td>
                                <Table.Td><Code title={a.manifest || a.command} style={{display: 'block', maxWidth: 400, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>{a.manifest ? manifestSummary(a) : a.command}</Code></Table.Td>
                                <Table.Td>
                                    {!a.manifest && <Switch aria-label={`${a.name} is global`} checked={a.global}
                                            onChange={e => UpdateApp({...a, global: e.currentTarget.checked}).then(reload, errMsg)}/>}
                                </Table.Td>
                                <Table.Td>
                                    <Group gap="xs" wrap="nowrap" justify="flex-end">
                                        {a.manifest && <ActionIcon variant="subtle" color="gray" aria-label="Export" title="Export manifest as .json" onClick={() => ExportApp(a.name).then(p => p && toast(`Exported to ${p}`), errMsg)}><IconDownload size={16}/></ActionIcon>}
                                        {!a.manifest && <>
                                            <ActionIcon variant="subtle" color="gray" aria-label="Test" title="Test on home folder" onClick={() => runApp(a, TestApp(a))}><IconPlayerPlay size={16}/></ActionIcon>
                                            <ActionIcon variant="subtle" color="gray" aria-label="Convert" title="Convert to manifest app" onClick={() => ConvertApp(a.name).then(reload, errMsg)}><IconTransform size={16}/></ActionIcon>
                                            <ActionIcon variant="subtle" color="gray" aria-label="Edit" onClick={() => setForm(a)}><IconPencil size={16}/></ActionIcon>
                                        </>}
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
                                onClick={() => InstallPreset(p).then(reload, errMsg)}>{p.name}</Button>
                    ))}
                </Group>
            </>}
            <AgentHooks/>
            <Modal opened={importing} onClose={() => setImporting(false)} title="Install app from manifest" size="lg">
                {importing && <ManifestForm onClose={saved => { setImporting(false); if (saved) reload() }}/>}
            </Modal>
            <Modal opened={!!form} onClose={() => setForm(undefined)} title={form?.id ? 'Edit app' : 'New app'} size="lg">
                {form && <AppForm initial={form} onClose={saved => { setForm(undefined); if (saved) reload() }}/>}
            </Modal>
        </Stack>
    )
}

const SLACK: Field[] = [
    {k: 'slack_bot_token', label: 'Bot token', desc: 'xoxb-... from OAuth & Permissions. Posts the DMs as a bot.', kind: 'secret'},
    {k: 'slack_user_token', label: 'User token (optional)', desc: 'xoxp-... Acts as you instead of the bot, only while "Act as me" is on.', kind: 'secret'},
    {k: 'slack_app_token', label: 'App token', desc: 'xapp-... with connections:write. Opens the Socket Mode connection, so no public URL is needed.', kind: 'secret'},
    {k: 'slack_user_id', label: 'Your Slack member ID', desc: 'U... (profile > Copy member ID). Only this user is DMed and obeyed; everyone else is ignored.'},
]
const STATUSES = ['needs_you', 'review', 'done', 'failed']

/** Chat bridge: DMs task status and takes replies, comments and /conveyor commands from a chat app (docs/slack.md). */
function Integrations() {
    const {data, error, reload} = useData(GetSettings, [])
    const set = (k: string, v: string) => SetSetting(k, v).then(reload, errMsg)
    return (
        <Stack>
            {error && <Text c="red">{error}</Text>}
            {data && (
                <Paper withBorder p="md" radius="md">
                    <Stack gap="sm">
                        <Group justify="space-between">
                            <Group gap="xs"><Title order={5}>Slack</Title>
                                <Badge variant="light" color={data.chat_enabled === 'on' && (data.slack_impersonate === 'on' && data.slack_user_token || data.slack_bot_token) && data.slack_app_token && data.slack_user_id ? 'green' : 'gray'}>
                                    {data.chat_enabled !== 'on' ? 'off' : (data.slack_impersonate === 'on' && data.slack_user_token || data.slack_bot_token) && data.slack_app_token && data.slack_user_id ? 'on' : 'needs tokens'}
                                </Badge>
                            </Group>
                            <Switch label="Send and listen" checked={data.chat_enabled === 'on'} onChange={e => set('chat_enabled', e.currentTarget.checked ? 'on' : 'off')}/>
                        </Group>
                        <Text size="sm" c="dimmed">
                            Answer needs_you questions, get PR links and run <Code>/conveyor list|comment|move</Code> from the Slack mobile app.
                            Create the Slack app as described in <Code>docs/slack.md</Code>. Changes apply without a restart.
                        </Text>
                        {SLACK.map(f => <SettingRow key={f.k} f={f} v={data[f.k] ?? ''} reload={reload}/>)}
                        <Switch label="Act as me (user token)" description="Post as you instead of the bot. Messages then land in your own self-DM and send no push notification."
                                checked={data.slack_impersonate === 'on'} onChange={e => set('slack_impersonate', e.currentTarget.checked ? 'on' : 'off')}/>
                        <MultiSelect label="Notify on" description="Task statuses that send a DM." data={STATUSES} value={(data.chat_statuses ?? '').split(',').filter(Boolean)}
                                     onChange={v => set('chat_statuses', v.join(','))}/>
                    </Stack>
                </Paper>
            )}
        </Stack>
    )
}

export function Apps() {
    return (
        <Tabs defaultValue="apps" keepMounted={false}>
            <Tabs.List mb="md"><Tabs.Tab value="apps">Apps</Tabs.Tab><Tabs.Tab value="integrations">Integrations</Tabs.Tab></Tabs.List>
            <Tabs.Panel value="apps"><AppList/></Tabs.Panel>
            <Tabs.Panel value="integrations"><Stack maw={900}><Title order={3}>Integrations</Title><Integrations/></Stack></Tabs.Panel>
        </Tabs>
    )
}
