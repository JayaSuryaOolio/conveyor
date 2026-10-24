import {useState} from 'react'
import {ActionIcon, Button, Checkbox, Code, Group, Modal, MultiSelect, ScrollArea, SegmentedControl, Select, Stack, Table, Tabs, Text, TextInput, Textarea, Title, Tooltip} from '@mantine/core'
import {IconAdjustments} from '@tabler/icons-react'
import {AddRepo, CreateTask, GetPromptDefaults, GetSettings, ListAgents, ListRepos, ListTasks, PreviewBrief, SetSetting, SetTaskAgent, SetTaskModel} from '../wailsjs/go/ui/App'
import {core} from '../wailsjs/go/models'
import {Nav, errMsg} from './components'
import {Markdown, MarkdownEditor} from './markdown'
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
                <ModelSelect value={model} onChange={setModel}/>
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
export function AgentSelect({repos, value, onChange, size}: {repos: string[]; value: string; onChange: (a: string) => void; size?: string}) {
    const {data} = useData(() => ListAgents(repos), [repos.join(',')])
    const names = [...new Set((data ?? []).map(a => a.name))]
    if (value && !names.includes(value)) names.push(value)
    return <Select size={size} placeholder="Agent: none" aria-label="Claude agent" clearable data={names} value={value || null} onChange={v => onChange(v ?? '')}/>
}

export function TaskAgent({k, repos, agent}: {k: string; repos: string[]; agent: string}) {
    return <AgentSelect size="xs" repos={repos} value={agent} onChange={a => SetTaskAgent(k, a).catch(errMsg)}/>
}

const MODELS = ['sonnet', 'opus', 'haiku']

/** Model choice; "" means the default (task: the agent's or default_model setting). A custom id already set is kept as an option. */
export function ModelSelect({value, onChange, label}: {value: string; onChange: (m: string) => void; label?: string}) {
    const data = [{value: '', label: 'Default'}, ...[...MODELS, ...(value && !MODELS.includes(value) ? [value] : [])].map(m => ({value: m, label: m}))]
    return <Select label={label} aria-label="Model" allowDeselect={false} value={value} onChange={v => onChange(v ?? '')} data={data}/>
}

/** Low-usage task settings behind one icon, so the sidebar stays short. */
export function TaskSettings({k, model, reload}: {k: string; model: string; reload: () => void}) {
    const [open, setOpen] = useState(false)
    return <>
        <Tooltip label="More settings">
            <ActionIcon variant="subtle" color="gray" aria-label="More settings" onClick={() => setOpen(true)}><IconAdjustments size={16}/></ActionIcon>
        </Tooltip>
        <Modal opened={open} onClose={() => setOpen(false)} title={`${k} settings`} size="sm">
            <ModelSelect label="Model" value={model} onChange={m => SetTaskModel(k, m).then(reload, errMsg)}/>
            <Text size="xs" c="dimmed" mt={4}>Used from the next run. Default: the agent's model, else the default_model setting.</Text>
        </Modal>
    </>
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

type Field = {k: string; label: string; desc: string; kind?: "area" | "md"}

const TABS: {name: string; fields: Field[]}[] = [
    {name: "General", fields: [
        {k: "key_prefix", label: "Task key prefix", desc: "Prefix for new task keys, e.g. CV gives CV-12."},
        {k: "default_model", label: "Default model", desc: "Model for runs that do not set one. Empty uses the Claude Code default."},
    ]},
    {name: "Dispatch", fields: [
        {k: "max_parallel", label: "Max parallel runs", desc: "How many tasks run at once."},
        {k: "resume_threshold", label: "Resume threshold (tokens)", desc: "A hot session resumes while its context peak is below this; above it a fresh session starts."},
        {k: "comment_debounce_secs", label: "Comment debounce (s)", desc: "Wait this long after a comment before starting a run, so you can batch comments."},
        {k: "max_consecutive_failures", label: "Max consecutive failures", desc: "After this many failed runs in a row the task is marked failed."},
    ]},
    {name: "External terminal", fields: [
        {k: "terminal_template", label: "External terminal command", desc: `Run by the external terminal button (terminal icon) on the Shell, Resume and New session rows of a task. {cwd} is the folder and {cmd} the command to run. The default opens macOS Terminal. iTerm: osascript -e 'tell application "iTerm" to create window with default profile command "cd {cwd} && {cmd}"'. Ghostty: open -na Ghostty --args --working-directory={cwd} -e sh -c "{cmd}". (The internal terminal icon runs inside Conveyor and ignores this.)`, kind: "area"},
    ]},
    {name: "Prompts", fields: [
        {k: "pr_comment_prefix", label: "PR comment prefix", desc: "Every PR or issue comment the agent posts must start with this. Added to the brief and enforced by a hook. Empty disables both."},
        {k: "brief_extra_rules", label: "Extra rules", desc: "Markdown appended to the Rules section of every brief.", kind: "md"},
        {k: "prompt_fresh", label: "Fresh prompt", desc: "First prompt of a new session. Placeholders: {key} {title} {brief} {comments}. Empty uses the built-in.", kind: "area"},
        {k: "prompt_resume", label: "Resume prompt", desc: "Prompt that resumes a session with new comments. Same placeholders. Empty uses the built-in.", kind: "area"},
    ]},
]

export function Settings() {
    const {data, error, reload} = useData(GetSettings, [])
    const {data: defs} = useData(GetPromptDefaults, [])
    const known = new Set(TABS.flatMap(t => t.fields.map(f => f.k)))
    const extra = Object.keys(data ?? {}).filter(k => !known.has(k)).sort().map((k): Field => ({k, label: k, desc: ""}))
    return (
        <Stack maw={800}>
            <Title order={3}>Settings</Title>
            {error && <Text c="red">{error}</Text>}
            {data && (
                <Tabs defaultValue="General" keepMounted={false}>
                    <Tabs.List mb="md">{TABS.map(t => <Tabs.Tab key={t.name} value={t.name}>{t.name}</Tabs.Tab>)}</Tabs.List>
                    {TABS.map(t => (
                        <Tabs.Panel key={t.name} value={t.name}>
                            <Stack>
                                {(t.name === "General" ? [...t.fields, ...extra] : t.fields).map(f => <SettingRow key={f.k} f={f} v={data[f.k] ?? ""} reload={reload} ph={f.k === "prompt_fresh" ? defs?.fresh : f.k === "prompt_resume" ? defs?.resume : undefined}/>)}
                                {t.name === "Prompts" && <PromptsInfo settings={data} protocol={defs?.protocol ?? ""}/>}
                            </Stack>
                        </Tabs.Panel>
                    ))}
                </Tabs>
            )}
        </Stack>
    )
}

function SettingRow({f, v, ph, reload}: {f: Field; v: string; ph?: string; reload: () => void}) {
    const [val, setVal] = useState(v)
    const props = {style: {flex: 1}, label: f.label, description: f.desc, placeholder: ph}
    return (
        <Group wrap="nowrap" align="flex-end">
            {f.kind === "md" ? <Stack gap={4} style={{flex: 1}}>
                <Text size="sm" fw={500}>{f.label}</Text><Text size="xs" c="dimmed">{f.desc}</Text>
                <MarkdownEditor minRows={4} value={val} onChange={setVal}/>
            </Stack>
                : f.kind === "area" ? <Textarea {...props} autosize minRows={2} maxRows={10} value={val} onChange={e => setVal(e.target.value)}/>
                : <TextInput {...props} value={val} onChange={e => setVal(e.target.value)}/>}
            <Button disabled={val === v} onClick={() => SetSetting(f.k, val).then(reload, errMsg)}>Save</Button>
        </Group>
    )
}

/** What the agent receives: the built-in protocol (read-only) and a live preview of a task brief. */
function PromptsInfo({settings, protocol}: {settings: Record<string, string>; protocol: string}) {
    const {data: tasks} = useData(ListTasks, [])
    const [key, setKey] = useState<string | null>(null)
    const [raw, setRaw] = useState("rendered")
    const sig = JSON.stringify(settings) // refetch when any setting is saved
    const {data: pv, error} = useData(() => key ? PreviewBrief(key) : Promise.resolve(undefined), [key, sig])
    return (
        <Stack mt="md">
            <Title order={5}>Built-in protocol and rules</Title>
            <Text size="xs" c="dimmed">Always in the brief. Read-only; add to the rules with Extra rules above.</Text>
            <ScrollArea.Autosize mah={360}><Code block>{protocol}</Code></ScrollArea.Autosize>
            <Title order={5}>Preview</Title>
            <Text size="xs" c="dimmed">The brief and first prompt for a task as they would be sent now. Nothing is written.</Text>
            <Group>
                <Select style={{flex: 1}} searchable placeholder="Pick a task" value={key} onChange={setKey}
                        data={(tasks ?? []).filter(t => t.status !== 'done' && t.status !== 'cancelled').sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)).map(t => ({value: t.key, label: `${t.key}  ${t.title}`}))}/>
                <SegmentedControl value={raw} onChange={setRaw} data={[{value: "rendered", label: "Rendered"}, {value: "raw", label: "Raw"}]}/>
            </Group>
            {error && <Text c="red">{error}</Text>}
            {pv && <>
                <Text size="sm" fw={600}>BRIEF.md</Text>
                {raw === "raw" ? <Code block>{pv.brief}</Code> : <Markdown>{pv.brief}</Markdown>}
                <Text size="sm" fw={600}>Fresh prompt</Text>
                <Code block>{pv.prompt}</Code>
            </>}
        </Stack>
    )
}
