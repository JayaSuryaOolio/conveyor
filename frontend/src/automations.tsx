import {useEffect, useState} from 'react'
import {ActionIcon, Button, Chip, Code, CopyButton, Group, Modal, MultiSelect, SegmentedControl, Select, Stack, Switch, Table, Text, TextInput, Textarea, Title, Tooltip} from '@mantine/core'
import {IconCheck, IconCopy, IconPencil, IconPlus, IconTrash} from '@tabler/icons-react'
import {AddAutomation, Confirm, DeleteAutomation, DescribeSchedule, ListAutomations, ListRepos, RunAutomation, SetAutomationEnabled, UpdateAutomation} from '../wailsjs/go/ui/App'
import {store, ui} from '../wailsjs/go/models'
import {Time, errMsg} from './components'
import {AgentSelect} from './forms'
import {ALL_STATUSES, STATUS_LABEL, useData} from './util'

const PRESETS = [['hourly', '0 * * * *'], ['weekdays 9am', '0 9 * * 1-5'], ['daily', '0 9 * * *'], ['Mondays', '0 9 * * 1']]

type Form = {
    name: string; schedule: string; precheck: string; kind: string
    title: string; repos: string[]; agent: string; description: string; start: boolean
    taskKey: string; status: string; body: string; args: string
}
const EMPTY: Form = {name: '', schedule: '0 9 * * 1-5', precheck: '', kind: 'create_task', title: '', repos: [], agent: '', description: '', start: false, taskKey: '', status: '', body: '', args: ''}

/** POSIX single-quote a string unless it is plainly safe. */
const shq = (s: string) => (/^[\w@%+=:,./-]+$/.test(s) ? s : "'" + s.replace(/'/g, "'\\''") + "'")

/** Split a command line into args, honouring single and double quotes. */
function splitArgs(s: string): string[] {
    return (s.match(/"[^"]*"|'[^']*'|\S+/g) ?? []).map(t => t.replace(/^(["'])(.*)\1$/, '$2'))
}

function actionArg(f: Form): object {
    if (f.kind === 'create_task') return {title: f.title, description: f.description, repos: f.repos, ...(f.agent && {agent: f.agent}), start: f.start}
    if (f.kind === 'comment') return f.status ? {task_status: f.status, body: f.body} : {task_key: f.taskKey, body: f.body}
    return {args: splitArgs(f.args)}
}

function toForm(a: store.Automation): Form {
    let x: any = {}
    try { x = JSON.parse(a.action_arg) } catch { /* edit the rest, leave action fields blank */ }
    return {
        ...EMPTY, name: a.name, schedule: a.schedule, precheck: a.precheck, kind: a.action_kind,
        title: x.title ?? '', repos: x.repos ?? [], agent: x.agent ?? '', description: x.description ?? '', start: !!x.start,
        taskKey: x.task_key ?? '', status: x.task_status ?? '', body: x.body ?? '', args: (x.args ?? []).map(shq).join(' '),
    }
}

function cliCommand(f: Form): string {
    const parts = ['conveyor automation add', shq(f.name || 'NAME'), '--schedule', shq(f.schedule)]
    if (f.precheck.trim()) parts.push('--precheck', shq(f.precheck))
    parts.push('--kind', f.kind, '--arg', shq(JSON.stringify(actionArg(f))))
    return parts.join(' ')
}

function AutomationForm({initial, edit, onClose}: {initial: Form; edit: boolean; onClose: (saved: boolean) => void}) {
    const [f, setF] = useState(initial)
    const set = (p: Partial<Form>) => setF(s => ({...s, ...p}))
    const {data: repos} = useData(ListRepos, [])
    const [info, setInfo] = useState<ui.ScheduleInfo>()
    useEffect(() => {
        const t = setTimeout(() => DescribeSchedule(f.schedule).then(setInfo, errMsg), 200)
        return () => clearTimeout(t)
    }, [f.schedule])
    const cmd = cliCommand(f)
    const ready = f.name.trim() && info?.valid && (f.kind === 'create_task' ? f.title.trim() && f.repos.length : f.kind === 'comment' ? f.body.trim() && (f.status || f.taskKey.trim()) : f.args.trim())
    const save = () => {
        const a = store.Automation.createFrom({name: f.name.trim(), schedule: f.schedule, precheck: f.precheck, action_kind: f.kind, action_arg: JSON.stringify(actionArg(f))})
        return (edit ? UpdateAutomation(a) : AddAutomation(a)).then(() => onClose(true), errMsg)
    }
    return (
        <Stack gap="sm">
            <TextInput data-autofocus label="Name" value={f.name} disabled={edit} onChange={e => set({name: e.target.value})}/>
            <TextInput label="Schedule (cron)" ff="monospace" value={f.schedule} onChange={e => set({schedule: e.target.value})}
                       error={f.schedule && info && !info.valid ? info.error : undefined}/>
            <Group gap="xs">
                {PRESETS.map(([l, x]) => <Chip key={x} size="xs" checked={f.schedule === x} onChange={() => set({schedule: x})}>{l}</Chip>)}
            </Group>
            {info?.valid && (
                <Text size="sm" c="dimmed">{info.description}. Next: {info.next.join(' · ')}</Text>
            )}
            <Textarea label="Precheck (optional)" description="Shell command. Exit 0 runs the action, non-zero skips it. Its stdout replaces {output} in the action text."
                      styles={{input: {fontFamily: 'monospace'}}} autosize minRows={1} maxRows={6} value={f.precheck} onChange={e => set({precheck: e.target.value})}/>
            <SegmentedControl fullWidth value={f.kind} onChange={kind => set({kind})} data={[{value: 'create_task', label: 'Create task'}, {value: 'comment', label: 'Comment'}, {value: 'cli', label: 'CLI'}]}/>
            {f.kind === 'create_task' && <>
                <TextInput label="Title" value={f.title} onChange={e => set({title: e.target.value})}/>
                <MultiSelect label="Repos" data={repos?.map(r => r.name) ?? []} value={f.repos} onChange={repos => set({repos})}/>
                <AgentSelect label="Agent" repos={f.repos} value={f.agent} onChange={agent => set({agent})}/>
                <Textarea label="Description" autosize minRows={3} maxRows={10} value={f.description} onChange={e => set({description: e.target.value})}/>
                <Switch label="Start the agent right away" checked={f.start} onChange={e => set({start: e.currentTarget.checked})}/>
            </>}
            {f.kind === 'comment' && <>
                <Select label="Comment on" description="One task by key, or every task in a status (the precheck then runs once per task, with CONVEYOR_TASK_KEY, _BRANCH and _PR set)."
                        allowDeselect={false} value={f.status || 'key'} onChange={v => set({status: !v || v === 'key' ? '' : v})}
                        data={[{value: 'key', label: 'A specific task'}, ...ALL_STATUSES.map(s => ({value: s, label: `All tasks in ${STATUS_LABEL[s]}`}))]}/>
                {!f.status && <TextInput label="Task key" value={f.taskKey} onChange={e => set({taskKey: e.target.value})}/>}
                <Textarea label="Body" autosize minRows={3} maxRows={10} value={f.body} onChange={e => set({body: e.target.value})}/>
            </>}
            {f.kind === 'cli' && <TextInput label="Conveyor args" description="Run as `conveyor ARGS`, e.g. task new --repo a" ff="monospace" value={f.args} onChange={e => set({args: e.target.value})}/>}
            <div>
                <Group justify="space-between"><Text size="sm" fw={500}>Equivalent CLI command</Text>
                    <CopyButton value={cmd}>{({copied, copy}) => (
                        <Tooltip label={copied ? 'Copied' : 'Copy'}><ActionIcon variant="subtle" color={copied ? 'teal' : 'gray'} onClick={copy} aria-label="Copy command">{copied ? <IconCheck size={16}/> : <IconCopy size={16}/>}</ActionIcon></Tooltip>
                    )}</CopyButton>
                </Group>
                <Code block style={{whiteSpace: 'pre-wrap', wordBreak: 'break-all'}}>{cmd}</Code>
            </div>
            <Group justify="flex-end">
                <Button variant="default" onClick={() => onClose(false)}>Cancel</Button>
                <Button onClick={save} disabled={!ready}>{edit ? 'Save' : 'Create'}</Button>
            </Group>
        </Stack>
    )
}

export function Automations() {
    const {data, error, reload} = useData(ListAutomations, [])
    const [form, setForm] = useState<{initial: Form; edit: boolean}>()
    return (
        <Stack>
            <Group justify="space-between">
                <Title order={3}>Automations</Title>
                <Button leftSection={<IconPlus size={16}/>} onClick={() => setForm({initial: EMPTY, edit: false})}>New automation</Button>
            </Group>
            {error && <Text c="red">{error}</Text>}
            {data && data.length === 0 && <Text c="dimmed">None yet. Create one, or use <Code>conveyor automation add --template pr-sweep</Code>.</Text>}
            {data && data.length > 0 && (
                <Table.ScrollContainer minWidth={700}>
                    <Table verticalSpacing="xs">
                        <Table.Thead><Table.Tr><Table.Th>Enabled</Table.Th><Table.Th>Name</Table.Th><Table.Th>Schedule</Table.Th><Table.Th>Kind</Table.Th><Table.Th>Last run</Table.Th><Table.Th>Result</Table.Th><Table.Th/></Table.Tr></Table.Thead>
                        <Table.Tbody>
                            {data.map(a => (
                                <Table.Tr key={a.name}>
                                    <Table.Td><Switch checked={a.enabled} onChange={e => SetAutomationEnabled(a.name, e.currentTarget.checked).then(reload, errMsg)}/></Table.Td>
                                    <Table.Td fw={600}>{a.name}</Table.Td>
                                    <Table.Td><Code>{a.schedule}</Code></Table.Td>
                                    <Table.Td>{a.action_kind}</Table.Td>
                                    <Table.Td>{a.last_run_at ? <Time iso={a.last_run_at}/> : ''}</Table.Td>
                                    <Table.Td><Text size="sm" lineClamp={1}>{a.last_result ?? ''}</Text></Table.Td>
                                    <Table.Td>
                                        <Group gap="xs" wrap="nowrap">
                                            <Button size="xs" variant="default" onClick={() => RunAutomation(a.name).then(reload, e => { errMsg(e); reload() })}>Run now</Button>
                                            <ActionIcon variant="subtle" color="gray" aria-label="Edit" onClick={() => setForm({initial: toForm(a), edit: true})}><IconPencil size={16}/></ActionIcon>
                                            <ActionIcon variant="subtle" color="red" aria-label="Delete" onClick={() => Confirm(`Delete automation ${a.name}?`).then(ok => { if (ok) return DeleteAutomation(a.name).then(reload) }).catch(errMsg)}><IconTrash size={16}/></ActionIcon>
                                        </Group>
                                    </Table.Td>
                                </Table.Tr>
                            ))}
                        </Table.Tbody>
                    </Table>
                </Table.ScrollContainer>
            )}
            <Modal opened={!!form} onClose={() => setForm(undefined)} title={form?.edit ? 'Edit automation' : 'New automation'} size="lg">
                {form && <AutomationForm initial={form.initial} edit={form.edit} onClose={saved => { setForm(undefined); if (saved) reload() }}/>}
            </Modal>
        </Stack>
    )
}
