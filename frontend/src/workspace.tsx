import {useEffect, useState} from 'react'
import {ActionIcon, Anchor, Badge, Button, Code, Divider, Group, Indicator, Paper, Select, Stack, Text, TextInput, Title, Tooltip} from '@mantine/core'
import {useClipboard} from '@mantine/hooks'
import {IconCheck, IconCopy, IconPencil, IconPlus, IconStar, IconTerminal, IconTerminal2, IconX} from '@tabler/icons-react'
import {Actions, AppIcons, EnsureWorkspace, Link, ListApps, ListTasks, OpenApp, RunAction, Sessions, SetTaskRepo, SpotlightRoot, SpotlightStop, TermStart, Unlink} from '../wailsjs/go/ui/App'
import {core, store, ui} from '../wailsjs/go/models'
import {BrowserOpenURL} from '../wailsjs/runtime/runtime'
import {AppIcon, Nav, StatusBadge, StatusSelect, Time, errMsg} from './components'
import {TaskAgent} from './forms'
import {useData} from './util'

const SPOTLIGHT = 'Spotlight'
const copy = (s: string) => navigator.clipboard.writeText(s).catch(errMsg)
const sq = (s: string) => "'" + s.replace(/'/g, "'\\''") + "'"
const cdCmd = (a: core.Action) => 'cd ' + sq(a.cwd) + (a.cmd ? ' && ' + a.cmd : '')

/** One full-width row: label left, copy / external terminal / internal terminal icons right. */
function ActionRow({k, a, kind, onTerm}: {k: string; a: core.Action; kind?: string; onTerm: (t: ui.TermInfo) => void}) {
    return (
        <Group justify="space-between" wrap="nowrap" gap="xs">
            <Text size="sm" style={{wordBreak: 'break-word'}}>{a.label}</Text>
            <Group gap={2} wrap="nowrap">
                <Tooltip label="Copy command">
                    <ActionIcon variant="default" size="sm" aria-label="Copy command" onClick={() => copy(cdCmd(a))}><IconCopy size={14}/></ActionIcon>
                </Tooltip>
                <Tooltip label="Open in external terminal">
                    <ActionIcon variant="default" size="sm" aria-label="Open in external terminal" onClick={() => RunAction(k, kind ?? a.kind, a.repo).catch(errMsg)}><IconTerminal2 size={14}/></ActionIcon>
                </Tooltip>
                <Tooltip label="Open in internal terminal">
                    <ActionIcon variant="default" size="sm" aria-label="Open in internal terminal"
                                onClick={() => TermStart(k, kind ?? a.kind, a.repo).then(onTerm).catch(errMsg)}><IconTerminal size={14}/></ActionIcon>
                </Tooltip>
            </Group>
        </Group>
    )
}

/** One icon button per configured app for a repo's worktree; flashes a check/cross after launching. Spotlight shows when it is active here and click stops it. */
function OpenIn({k, repo, wt, apps, icons}: {k: string; repo: string; wt: string; apps: store.App[]; icons?: Record<string, string>}) {
    const [root, setRoot] = useState('')
    const [res, setRes] = useState<Record<string, boolean>>({})
    const hasSpotlight = apps.some(a => a.name === SPOTLIGHT)
    const check = () => { if (hasSpotlight) SpotlightRoot(wt).then(setRoot) }
    useEffect(check, [wt, hasSpotlight]) // eslint-disable-line react-hooks/exhaustive-deps
    const flash = (n: string, ok: boolean) => {
        setRes(r => ({...r, [n]: ok}))
        setTimeout(() => setRes(({[n]: _, ...r}) => r), 1500)
    }
    const click = (a: store.App) => (a.name === SPOTLIGHT && root ? SpotlightStop(root) : OpenApp(a.name, k, repo))
        .then(() => flash(a.name, true), e => { flash(a.name, false); errMsg(e) }).finally(check)
    if (apps.length === 0) return <Text size="xs" c="dimmed">No apps yet. Add them on the Apps page.</Text>
    return (
        <Group gap={4}>
            {apps.map(a => {
                const active = a.name === SPOTLIGHT && !!root
                return (
                    <Tooltip key={a.id} label={active ? 'Spotlight active – click to stop' : a.name}>
                        <Indicator disabled={!active} color="green" size={8} processing>
                            <ActionIcon variant="default" size="md" aria-label={a.name} onClick={() => click(a)}>
                                {a.name in res ? (res[a.name] ? <IconCheck size={16} color="var(--mantine-color-green-6)"/> : <IconX size={16} color="var(--mantine-color-red-6)"/>) : <AppIcon name={a.name} icons={icons}/>}
                            </ActionIcon>
                        </Indicator>
                    </Tooltip>
                )
            })}
        </Group>
    )
}

const PR_COLOR: Record<string, string> = {open: 'green', merged: 'violet', closed: 'red', draft: 'gray'}

/** Full value, wrapped anywhere; click copies it and flashes "Copied". */
function Mono({text}: {text: string}) {
    const cb = useClipboard({timeout: 1200})
    return (
        <Tooltip label="Copied" opened={cb.copied} position="top-start">
            <Code style={{display: 'block', cursor: 'pointer', whiteSpace: 'pre-wrap', wordBreak: 'break-all'}} title="Click to copy" onClick={() => cb.copy(text)}>{text}</Code>
        </Tooltip>
    )
}

// RepoFields shows one repo's branch, worktree and PR. The branch is editable
// until the worktree exists; the PR URL via the pencil.
function RepoFields({k, r, reload, children}: {k: string; r: store.TaskRepo; reload: () => void; children?: React.ReactNode}) {
    const [branch, setBranch] = useState(r.branch)
    const [pr, setPr] = useState(r.pr_url ?? '')
    const [editPr, setEditPr] = useState(false)
    const dirty = branch !== r.branch || pr !== (r.pr_url ?? '')
    const save = () => SetTaskRepo(k, r.repo_name, branch, pr).then(reload).catch(errMsg)
    const onKey = (e: React.KeyboardEvent) => { if (e.key === 'Enter' && dirty) save() }
    return (
        <Paper withBorder p="xs" radius="md">
            <Stack gap="xs">
                <Group gap={6}>
                    <Text size="sm" fw={600}>{r.repo_name}</Text>
                    {r.is_primary && <Badge size="xs" variant="light" leftSection={<IconStar size={10}/>}>primary</Badge>}
                </Group>
                <div>
                    <Text size="xs" c="dimmed">Branch</Text>
                    {r.worktree_path
                        ? <Mono text={r.branch}/>
                        : <TextInput size="xs" value={branch} onChange={e => setBranch(e.target.value)} onKeyDown={onKey}/>}
                </div>
                <div>
                    <Text size="xs" c="dimmed">Worktree</Text>
                    {r.worktree_path ? <Mono text={r.worktree_path}/> : <Text size="xs" c="dimmed">not created yet</Text>}
                </div>
                <div>
                    <Group gap={6} mb={2}>
                        <Text size="xs" c="dimmed">PR</Text>
                        {r.pr_url && r.pr_state && <Badge size="xs" variant="light" color={PR_COLOR[r.pr_state.toLowerCase()] ?? 'gray'}>{r.pr_state}</Badge>}
                        {r.pr_url && <Anchor size="xs" href={r.pr_url} onClick={e => { e.preventDefault(); BrowserOpenURL(r.pr_url!) }}>Open PR</Anchor>}
                        <Tooltip label="Edit PR URL">
                            <ActionIcon variant="subtle" color="gray" size="xs" aria-label="Edit PR URL" onClick={() => setEditPr(!editPr)}><IconPencil size={12}/></ActionIcon>
                        </Tooltip>
                    </Group>
                    {editPr || !r.pr_url
                        ? <TextInput size="xs" value={pr} placeholder="https://github.com/…/pull/1" onChange={e => setPr(e.target.value)} onKeyDown={onKey}/>
                        : <Mono text={r.pr_url}/>}
                </div>
                {dirty && <Button size="compact-sm" onClick={save}>Save</Button>}
                {children}
            </Stack>
        </Paper>
    )
}

// Link choices as seen from this task: [value, label, kind, this task is the link's from side].
const LINK_CHOICES: [string, string, string, boolean][] = [
    ['blocks', 'blocks', 'blocks', true], ['blocked', 'is blocked by', 'blocks', false],
    ['relates', 'relates to', 'relates', true],
    ['duplicates', 'duplicates', 'duplicates', true], ['duplicated', 'is duplicated by', 'duplicates', false],
]

/** Parent, sub-tasks and links of a task, with add / remove for links. */
function Relations({k, status, rel, nav, reload}: {k: string; status: string; rel: store.Relations; nav: Nav; reload: () => void}) {
    const {data: tasks} = useData(ListTasks, [])
    const [choice, setChoice] = useState('blocks')
    const [other, setOther] = useState<string | null>(null)
    const open = (s: string) => s !== 'done' && s !== 'cancelled'
    const blockers = rel.links.filter(l => l.kind === 'blocks' && !l.from && open(l.task.status))
    const link = (task: store.TaskRef) => (
        <Anchor size="xs" component="button" ta="left" onClick={() => nav({name: 'task', key: task.key})}>{task.key}</Anchor>
    )
    const add = () => {
        const [, , kind, from] = LINK_CHOICES.find(c => c[0] === choice)!
        if (other) (from ? Link(k, kind, other) : Link(other, kind, k)).then(() => { setOther(null); reload() }).catch(errMsg)
    }
    const labels = [...new Set(rel.links.map(l => l.label))]
    return (
        <Stack gap={6}>
            <Title order={6}>Relations</Title>
            {status === 'todo' && blockers.length > 0 && <Text size="xs" c="orange">Not launched: blocked by {blockers.map(l => l.task.key).join(', ')}</Text>}
            {rel.parent && <Group gap={6} wrap="nowrap"><Text size="xs" c="dimmed">Parent</Text>{link(rel.parent)}<StatusBadge status={rel.parent.status}/></Group>}
            {rel.children.length > 0 && <Text size="xs" c="dimmed">Sub-tasks</Text>}
            {rel.children.map(c => (
                <Group key={c.key} gap={6} wrap="nowrap" justify="space-between">
                    <Group gap={6} wrap="nowrap" miw={0}>{link(c)}<Text size="xs" lineClamp={1} title={c.title}>{c.title}</Text></Group>
                    <StatusBadge status={c.status}/>
                </Group>
            ))}
            {labels.map(label => (
                <Stack key={label} gap={2}>
                    <Text size="xs" c="dimmed">{label}</Text>
                    {rel.links.filter(l => l.label === label).map(l => (
                        <Group key={l.kind + l.task.key} gap={6} wrap="nowrap" justify="space-between">
                            <Group gap={6} wrap="nowrap" miw={0}>{link(l.task)}<Text size="xs" lineClamp={1} title={l.task.title}>{l.task.title}</Text></Group>
                            <Group gap={4} wrap="nowrap">
                                <StatusBadge status={l.task.status}/>
                                <ActionIcon variant="subtle" color="gray" size="xs" aria-label="Remove link"
                                            onClick={() => (l.from ? Unlink(k, l.kind, l.task.key) : Unlink(l.task.key, l.kind, k)).then(reload).catch(errMsg)}><IconX size={12}/></ActionIcon>
                            </Group>
                        </Group>
                    ))}
                </Stack>
            ))}
            <Group gap={4} wrap="nowrap">
                <Select size="xs" w={118} allowDeselect={false} value={choice} onChange={v => v && setChoice(v)} data={LINK_CHOICES.map(c => ({value: c[0], label: c[1]}))}/>
                <Select size="xs" searchable clearable placeholder="Task" style={{flex: 1}} value={other} onChange={setOther}
                        data={(tasks ?? []).filter(x => x.key !== k).map(x => ({value: x.key, label: `${x.key} ${x.title}`}))}/>
                <ActionIcon variant="default" size="md" aria-label="Add link" disabled={!other} onClick={add}><IconPlus size={14}/></ActionIcon>
            </Group>
        </Stack>
    )
}

export function TaskSidebar({t, rel, runs, nav, reload, onTerm}: {t: store.Task; rel: store.Relations; runs: store.Run[]; nav: Nav; reload: () => void; onTerm: (t: ui.TermInfo) => void}) {
    const k = t.key
    const repos = t.repos ?? []
    const {data: actions, error: actionsError, reload: reloadActions} = useData(() => Actions(k), [k])
    const {data: sessions, error: sessionsError, reload: reloadSessions} = useData(() => Sessions(k), [k])
    const {data: apps} = useData(ListApps, [])
    const {data: icons} = useData(AppIcons, [])
    const [busy, setBusy] = useState(false)
    const taskActions = (actions ?? []).filter(a => ['attach', 'resume', 'fresh'].includes(a.kind))
    const hasWorktree = repos.some(r => r.worktree_path)
    return (
        <Stack gap="sm" component="aside" miw={0}>
            <Group justify="space-between" wrap="nowrap">
                <Text size="sm" fw={600}>Agent</Text>
                <TaskAgent k={k} repos={repos.map(r => r.repo_name)} agent={t.agent ?? ''}/>
            </Group>
            <Group justify="space-between">
                <Text size="sm" fw={600}>Status</Text>
                <StatusSelect k={k} status={t.status}/>
            </Group>
            <Group justify="space-between"><Text size="xs" c="dimmed">Created</Text><Time iso={t.created_at}/></Group>
            <Group justify="space-between"><Text size="xs" c="dimmed">Updated</Text><Time iso={t.updated_at}/></Group>
            <Divider/>
            <Relations k={k} status={t.status} rel={rel} nav={nav} reload={reload}/>
            <Divider/>
            <Title order={6}>Repos</Title>
            {actionsError && <Text size="xs" c="red">Failed to load actions: {actionsError}</Text>}
            {repos.map(r => (
                <RepoFields key={r.repo_id + '|' + r.branch + '|' + (r.pr_url ?? '')} k={k} r={r} reload={reload}>
                    {(actions ?? []).filter(a => a.repo === r.repo_name && a.kind === 'shell').map(a => <ActionRow key={a.kind} k={k} a={a} onTerm={onTerm}/>)}
                    {r.worktree_path && <OpenIn k={k} repo={r.repo_name} wt={r.worktree_path} apps={apps ?? []} icons={icons}/>}
                </RepoFields>
            ))}
            {!hasWorktree && (
                <Button loading={busy} onClick={() => { setBusy(true); EnsureWorkspace(k).then(() => { reload(); reloadActions(); reloadSessions() }).catch(errMsg).finally(() => setBusy(false)) }}>Create workspace</Button>
            )}
            {taskActions.map(a => <ActionRow key={a.kind} k={k} a={a} onTerm={onTerm}/>)}
            <Divider/>
            <Title order={6}>Sessions</Title>
            {sessionsError && <Text size="xs" c="red">Failed to load sessions: {sessionsError}</Text>}
            {runs.length + (sessions?.length ?? 0) === 0 && <Text size="xs" c="dimmed">None yet.</Text>}
            {runs.map((r, i) => (
                <Anchor key={r.id} size="xs" c="dimmed" component="button" ta="left" title="Show in thread"
                        onClick={() => document.getElementById('run-' + r.id)?.scrollIntoView({behavior: 'smooth', block: 'start'})}>
                    #{i + 1} {r.kind} · {r.status}{r.block_status ? ' · ' + r.block_status : ''}
                </Anchor>
            ))}
            {sessions?.map(s => (
                <Stack key={s.id} gap={2}>
                    <ActionRow k={k} a={s.action} kind={'resume:' + s.id} onTerm={onTerm}/>
                    <Text size="xs" c="dimmed" lineClamp={1} title={s.prompt}>{s.prompt || s.id}</Text>
                </Stack>
            ))}
        </Stack>
    )
}
