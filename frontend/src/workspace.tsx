import {useEffect, useState} from 'react'
import {ActionIcon, Anchor, Badge, Button, Code, Divider, Group, Indicator, Paper, Stack, TagsInput, Text, TextInput, Title, Tooltip} from '@mantine/core'
import {useClipboard} from '@mantine/hooks'
import {IconCheck, IconCopy, IconPencil, IconStar, IconTerminal, IconTerminal2, IconX} from '@tabler/icons-react'
import {Actions, AddLabel, AppIcons, EnsureWorkspace, GetSettings, ListApps, ListLabels, RemoveLabel, OpenApp, RunAction, Sessions, SetTaskRepo, SpotlightRoot, SpotlightStop, TermStart} from '../wailsjs/go/ui/App'
import {core, store, ui} from '../wailsjs/go/models'
import {BrowserOpenURL} from '../wailsjs/runtime/runtime'
import {AppIcon, StatusSelect, Time, errMsg, runApp} from './components'
import {TaskAgent, TaskSettings} from './forms'
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
            <ActionIcons k={k} a={a} kind={kind} onTerm={onTerm}/>
        </Group>
    )
}

/** Copy / external terminal / internal terminal icons for one action. */
function ActionIcons({k, a, kind, onTerm}: {k: string; a: core.Action; kind?: string; onTerm: (t: ui.TermInfo) => void}) {
    return (
        <>
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
        </>
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
    const click = (a: store.App) => (a.name === SPOTLIGHT && root ? SpotlightStop(root) : OpenApp(a.name, k, repo).then(() => {}))
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

/** Output apps run on the task's primary worktree; their output shows as a toast. */
function QuickActions({k, apps, icons}: {k: string; apps: store.App[]; icons?: Record<string, string>}) {
    const [busy, setBusy] = useState('')
    if (apps.length === 0) return null
    return (
        <Group gap={4}>
            {apps.map(a => (
                <Button key={a.id} size="xs" variant="default" loading={busy === a.name} leftSection={<AppIcon name={a.name} icons={icons} size={14}/>}
                        onClick={() => { setBusy(a.name); runApp(a, OpenApp(a.name, k, '')).finally(() => setBusy('')) }}>{a.name}</Button>
            ))}
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

/** Who made the task: the exact created_by, else (rows from before it existed) inferred from the lettered key (CV-14A is a child of CV-14). */
function creator(key: string, source: string, user: string | undefined, createdBy?: string | null) {
    if (createdBy) {
        const [kind, ...rest] = createdBy.split(':'), arg = rest.join(':')
        if (kind === 'human') return user || 'human'
        if (kind === 'agent') return `agent · ${arg}`
        if (kind === 'followup') return `follow-up of ${arg}`
        if (kind === 'automation') return `automation · ${arg}`
        return createdBy
    }
    const parent = key.match(/^(.+-\d+)[A-Z]+$/)?.[1]
    if (parent) return source === 'followup' ? `follow-up of ${parent}` : `agent · ${parent}`
    if (source === 'jira' || source === 'learning') return source
    return user || 'human'
}

/** Label picker: choose existing labels from the dropdown or type a new one and press Enter. */
function Labels({k, labels, reload}: {k: string; labels: string[]; reload: () => void}) {
    const {data: all, reload: reloadAll} = useData(ListLabels, [])
    const change = (next: string[]) => {
        const lower = (xs: string[]) => xs.map(x => x.toLowerCase())
        const adds = next.filter(n => !lower(labels).includes(n.toLowerCase()))
        const drops = labels.filter(l => !lower(next).includes(l.toLowerCase()))
        Promise.all([...adds.map(n => AddLabel(k, n)), ...drops.map(n => RemoveLabel(k, n))]).then(() => { reload(); reloadAll() }).catch(errMsg)
    }
    return <TagsInput size="xs" label="Labels" placeholder="Add label…" value={labels} data={all ?? []} onChange={change} clearable={false} splitChars={[',']}/>
}

export function TaskSidebar({t, runs, reload, onTerm}: {t: store.Task; runs: store.Run[]; reload: () => void; onTerm: (t: ui.TermInfo) => void}) {
    const k = t.key
    const repos = t.repos ?? []
    const {data: actions, error: actionsError, reload: reloadActions} = useData(() => Actions(k), [k])
    const {data: sessions, error: sessionsError, reload: reloadSessions} = useData(() => Sessions(k), [k])
    const {data: apps} = useData(ListApps, [])
    const {data: settings} = useData(GetSettings, [])
    const by = creator(k, t.source, settings?.user_name, t.created_by)
    const {data: icons} = useData(AppIcons, [])
    const [busy, setBusy] = useState(false)
    const taskActions = (actions ?? []).filter(a => ['attach', 'fresh', 'shell'].includes(a.kind))
    const resume = new Map((actions ?? []).filter(a => /^(resume|attach):/.test(a.kind)).map(a => [a.kind.split(':')[1], a]))
    const hasWorktree = repos.some(r => r.worktree_path)
    return (
        <Stack gap="sm" component="aside" miw={0}>
            <Group justify="space-between" wrap="nowrap">
                <Group gap={4}><Text size="sm" fw={600}>Agent</Text><TaskSettings k={k} model={t.model ?? ''} reload={reload}/></Group>
                <TaskAgent k={k} repos={repos.map(r => r.repo_name)} agent={t.agent ?? ''}/>
            </Group>
            <Group justify="space-between">
                <Text size="sm" fw={600}>Status</Text>
                <StatusSelect k={k} status={t.status}/>
            </Group>
            <Group justify="space-between"><Text size="xs" c="dimmed">Created</Text><Group gap={4} wrap="nowrap"><Text size="xs">{by} ·</Text><Time iso={t.created_at}/></Group></Group>
            <Group justify="space-between"><Text size="xs" c="dimmed">Updated</Text><Time iso={t.updated_at}/></Group>
            <Labels k={k} labels={t.labels ?? []} reload={reload}/>
            <Divider/>
            <Title order={6}>Repos</Title>
            {actionsError && <Text size="xs" c="red">Failed to load actions: {actionsError}</Text>}
            {repos.map(r => (
                <RepoFields key={r.repo_id + '|' + r.branch + '|' + (r.pr_url ?? '')} k={k} r={r} reload={reload}/>
            ))}
            {!hasWorktree && (
                <Button loading={busy} onClick={() => { setBusy(true); EnsureWorkspace(k).then(() => { reload(); reloadActions(); reloadSessions() }).catch(errMsg).finally(() => setBusy(false)) }}>Create workspace</Button>
            )}
            {taskActions.map(a => <ActionRow key={a.kind + a.repo} k={k} a={a} onTerm={onTerm}/>)}
            {/* refactor: OpenIn is per worktree, so one labelled row per repo; a task-level app launcher would drop the labels */}
            {repos.filter(r => r.worktree_path).map(r => (
                <Stack key={r.repo_id} gap={2}>
                    {repos.length > 1 && <Text size="xs" c="dimmed">{r.repo_name}</Text>}
                    <OpenIn k={k} repo={r.repo_name} wt={r.worktree_path!} apps={(apps ?? []).filter(a => !a.global && !a.output)} icons={icons}/>
                </Stack>
            ))}
            {hasWorktree && <QuickActions k={k} apps={(apps ?? []).filter(a => !a.global && a.output)} icons={icons}/>}
            <Divider/>
            <Title order={6}>Sessions</Title>
            {sessionsError && <Text size="xs" c="red">Failed to load sessions: {sessionsError}</Text>}
            {runs.length + (sessions?.length ?? 0) === 0 && <Text size="xs" c="dimmed">None yet.</Text>}
            {runs.map((r, i) => (
                <Group key={r.id} justify="space-between" wrap="nowrap" gap="xs">
                    <Anchor size="xs" c="dimmed" component="button" ta="left" title="Show in thread"
                            onClick={() => document.getElementById('run-' + r.id)?.scrollIntoView({behavior: 'smooth', block: 'start'})}>
                        #{i + 1} {r.kind} · {r.status}{r.block_status ? ' · ' + r.block_status : ''}
                    </Anchor>
                    {resume.get(r.session_id ?? '') && <ActionIcons k={k} a={resume.get(r.session_id!)!} onTerm={onTerm}/>}
                </Group>
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
