import {useEffect, useState} from 'react'
import {ActionIcon, Chip, Code, CopyButton, MultiSelect, Popover, SegmentedControl, Select, Switch, Table, TextInput, Textarea, Title, Tooltip} from '@mantine/core'
import {Button, Group, Modal, Stack, Text} from './ui'
import {IconCheck, IconCopy, IconCalendarTime, IconPencil, IconPlus, IconTrash} from '@tabler/icons-react'
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
                <Text size="sm" tone="muted">{info.description}. Next: {info.next.join(' · ')}</Text>
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
                <Group justify="between"><Text size="sm" weight={500}>Equivalent CLI command</Text>
                    <CopyButton value={cmd}>{({copied, copy}) => (
                        <Tooltip label={copied ? 'Copied' : 'Copy'}><ActionIcon variant="subtle" color={copied ? 'teal' : 'gray'} onClick={copy} aria-label="Copy command">{copied ? <IconCheck size={16}/> : <IconCopy size={16}/>}</ActionIcon></Tooltip>
                    )}</CopyButton>
                </Group>
                <Code block style={{whiteSpace: 'pre-wrap', wordBreak: 'break-all'}}>{cmd}</Code>
            </div>
            <Group justify="end">
                <Button variant="secondary" onClick={() => onClose(false)}>Cancel</Button>
                <Button onClick={save} disabled={!ready}>{edit ? 'Save' : 'Create'}</Button>
            </Group>
        </Stack>
    )
}

type PastRun = {at: string; result: string}
const PAST_CAP = 10

/** Completed runs the server reports are remembered per automation in localStorage (it keeps only the latest). */
function usePastRuns(name: string, at?: string | null, result?: string | null): PastRun[] {
    const key = 'automation-runs:' + name
    const read = (): PastRun[] => { try { return JSON.parse(localStorage.getItem(key) ?? '[]') } catch { return [] } }
    const [runs, setRuns] = useState(read)
    useEffect(() => {
        if (!at) return
        const cur = read()
        if (cur.some(r => r.at === at)) return
        const next = [{at, result: result ?? ''}, ...cur].slice(0, PAST_CAP)
        try { localStorage.setItem(key, JSON.stringify(next)) } catch { /* private mode */ }
        setRuns(next)
    }, [key, at, result])
    return runs
}

function NextRuns({schedule, name, lastRunAt, lastResult}: {schedule: string; name: string; lastRunAt?: string | null; lastResult?: string | null}) {
    const [info, setInfo] = useState<ui.ScheduleInfo>()
    const past = usePastRuns(name, lastRunAt, lastResult)
    return (
        <Popover width={300} withArrow position="bottom">
            <Popover.Target>
                <ActionIcon variant="subtle" color="gray" aria-label="Next runs" onClick={() => DescribeSchedule(schedule).then(setInfo, errMsg)}><IconCalendarTime size={16}/></ActionIcon>
            </Popover.Target>
            <Popover.Dropdown>
                {past.length > 0 && <>
                    <Text size="xs" tone="muted" style={{marginBottom: 4}}>Completed runs</Text>
                    {past.map(r => <Group key={r.at} gap={6} wrap={false}><Time iso={r.at}/><Text size="xs" tone="muted" clamp={1}>{r.result}</Text></Group>)}
                    <Text size="xs" tone="muted" style={{marginTop: 10, marginBottom: 4}}>Next runs</Text>
                </>}
                {past.length === 0 && <Text size="xs" tone="muted" style={{marginBottom: 4}}>Next runs</Text>}
                {info?.valid ? info.next.map(t => <Text key={t} size="sm">{t}</Text>) : <Text size="sm" tone="danger">{info?.error}</Text>}
            </Popover.Dropdown>
        </Popover>
    )
}

function RunButton({name, onDone}: {name: string; onDone: () => void}) {
    const [state, setState] = useState<'idle' | 'running' | 'ran'>('idle')
    const run = () => {
        setState('running')
        RunAutomation(name)
            .then(() => { setState('ran'); setTimeout(() => setState('idle'), 2500) }, e => { errMsg(e); setState('idle') })
            .finally(onDone)
    }
    return <Button size="xs" variant="secondary" loading={state === 'running'} tone={state === 'ran' ? 'success' : undefined} onClick={run}>{state === 'ran' ? 'Ran ✓' : 'Run now'}</Button>
}

export function Automations() {
    const {data, error, reload} = useData(ListAutomations, [])
    const [form, setForm] = useState<{initial: Form; edit: boolean}>()
    return (
        <Stack>
            <Group justify="between">
                <Title order={3}>Automations</Title>
                <Button icon={<IconPlus size={16}/>} onClick={() => setForm({initial: EMPTY, edit: false})}>New automation</Button>
            </Group>
            {error && <Text tone="danger">{error}</Text>}
            {data && data.length === 0 && <Text tone="muted">None yet. Create one, or use <Code>conveyor automation add --template pr-sweep</Code>.</Text>}
            {data && data.length > 0 && (
                <Table.ScrollContainer minWidth={700}>
                    <Table verticalSpacing="xs">
                        <Table.Thead><Table.Tr><Table.Th>Enabled</Table.Th><Table.Th>Name</Table.Th><Table.Th>Schedule</Table.Th><Table.Th>Kind</Table.Th><Table.Th>Last run</Table.Th><Table.Th>Result</Table.Th><Table.Th/></Table.Tr></Table.Thead>
                        <Table.Tbody>
                            {data.map(a => (
                                <Table.Tr key={a.name}>
                                    <Table.Td><Switch checked={a.enabled} onChange={e => SetAutomationEnabled(a.name, e.currentTarget.checked).then(reload, errMsg)}/></Table.Td>
                                    <Table.Td fw={600}>{a.name}</Table.Td>
                                    <Table.Td>{a.schedule ? <Group gap={4} wrap={false}><Code>{a.schedule}</Code><NextRuns schedule={a.schedule} name={a.name} lastRunAt={a.last_run_at} lastResult={a.last_result}/></Group> : a.run_at ? <Group gap={4} wrap={false}><Text size="sm" tone="muted">once</Text><Time iso={a.run_at}/></Group> : null}</Table.Td>
                                    <Table.Td>{a.action_kind}</Table.Td>
                                    <Table.Td>{a.last_run_at ? <Time iso={a.last_run_at}/> : ''}</Table.Td>
                                    <Table.Td><Text size="sm" clamp={1}>{a.last_result ?? ''}</Text></Table.Td>
                                    <Table.Td>
                                        <Group gap="xs" wrap={false}>
                                            <RunButton name={a.name} onDone={reload}/>
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
