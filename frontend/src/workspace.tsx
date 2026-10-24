import {useState} from 'react'
import {ActionIcon, Anchor, Badge, Button, Code, CopyButton, Divider, Group, Paper, Stack, Text, TextInput, Title, Tooltip} from '@mantine/core'
import {IconCheck, IconCopy, IconStar} from '@tabler/icons-react'
import {Actions, EnsureWorkspace, RunAction, Sessions, SetTaskRepo} from '../wailsjs/go/ui/App'
import {core, store} from '../wailsjs/go/models'
import {BrowserOpenURL} from '../wailsjs/runtime/runtime'
import {StatusSelect, Time, errMsg} from './components'
import {useData} from './util'

const copy = (s: string) => navigator.clipboard.writeText(s).catch(errMsg)
const sq = (s: string) => "'" + s.replace(/'/g, "'\\''") + "'"
const cdCmd = (a: core.Action) => 'cd ' + sq(a.cwd) + (a.cmd && a.kind !== 'editor' ? ' && ' + a.cmd : '')

function ActionButton({k, a, kind}: {k: string; a: core.Action; kind?: string}) {
    return (
        <Group gap={2} wrap="nowrap">
            <Button variant="default" size="compact-sm" onClick={() => RunAction(k, kind ?? a.kind, a.repo).catch(errMsg)}>{a.label}</Button>
            <Tooltip label="Copy command">
                <ActionIcon variant="default" size="sm" aria-label="Copy command" onClick={() => copy(a.kind === 'editor' ? a.cmd : cdCmd(a))}><IconCopy size={14}/></ActionIcon>
            </Tooltip>
        </Group>
    )
}

const PR_COLOR: Record<string, string> = {open: 'green', merged: 'violet', closed: 'red', draft: 'gray'}

/** Single-line value that truncates with ellipsis, full text on hover, with an inline copy button. */
function Mono({text}: {text: string}) {
    return (
        <Group gap={4} wrap="nowrap">
            <Tooltip label={text} multiline maw={300} openDelay={300}>
                <Code style={{flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>{text}</Code>
            </Tooltip>
            <CopyButton value={text}>
                {({copied, copy}) => (
                    <ActionIcon variant="subtle" color={copied ? 'teal' : 'gray'} size="sm" aria-label="Copy" onClick={copy}>
                        {copied ? <IconCheck size={14}/> : <IconCopy size={14}/>}
                    </ActionIcon>
                )}
            </CopyButton>
        </Group>
    )
}

// RepoFields shows one repo's branch, worktree and PR. The branch is editable
// until the worktree exists; the PR URL always is.
function RepoFields({k, r, reload, children}: {k: string; r: store.TaskRepo; reload: () => void; children?: React.ReactNode}) {
    const [branch, setBranch] = useState(r.branch)
    const [pr, setPr] = useState(r.pr_url ?? '')
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
                    </Group>
                    <TextInput size="xs" value={pr} placeholder="https://github.com/…/pull/1" onChange={e => setPr(e.target.value)} onKeyDown={onKey}/>
                </div>
                {dirty && <Button size="compact-sm" onClick={save}>Save</Button>}
                <Group gap="xs">{children}</Group>
            </Stack>
        </Paper>
    )
}

export function TaskSidebar({t, runs, reload}: {t: store.Task; runs: store.Run[]; reload: () => void}) {
    const k = t.key
    const repos = t.repos ?? []
    const {data: actions, reload: reloadActions} = useData(() => Actions(k), [k])
    const {data: sessions, reload: reloadSessions} = useData(() => Sessions(k), [k])
    const [busy, setBusy] = useState(false)
    const taskActions = (actions ?? []).filter(a => ['attach', 'resume', 'fresh'].includes(a.kind))
    const hasWorktree = repos.some(r => r.worktree_path)
    return (
        <Stack gap="sm" component="aside" miw={0}>
            <Group justify="space-between">
                <Text size="sm" fw={600}>Status</Text>
                <StatusSelect k={k} status={t.status}/>
            </Group>
            <Group justify="space-between"><Text size="xs" c="dimmed">Created</Text><Time iso={t.created_at}/></Group>
            <Group justify="space-between"><Text size="xs" c="dimmed">Updated</Text><Time iso={t.updated_at}/></Group>
            <Divider/>
            <Title order={6}>Repos</Title>
            {repos.map(r => (
                <RepoFields key={r.repo_id + '|' + r.branch + '|' + (r.pr_url ?? '')} k={k} r={r} reload={reload}>
                    {(actions ?? []).filter(a => a.repo === r.repo_name && ['shell', 'editor'].includes(a.kind)).map(a => <ActionButton key={a.kind} k={k} a={a}/>)}
                </RepoFields>
            ))}
            {!hasWorktree && (
                <Button loading={busy} onClick={() => { setBusy(true); EnsureWorkspace(k).then(() => { reload(); reloadActions(); reloadSessions() }).catch(errMsg).finally(() => setBusy(false)) }}>Create workspace</Button>
            )}
            {taskActions.length > 0 && <Group gap="xs">{taskActions.map(a => <ActionButton key={a.kind} k={k} a={a}/>)}</Group>}
            <Divider/>
            <Title order={6}>Sessions</Title>
            {runs.length + (sessions?.length ?? 0) === 0 && <Text size="xs" c="dimmed">None yet.</Text>}
            {runs.map((r, i) => (
                <Text key={r.id} size="xs" c="dimmed">#{i + 1} {r.kind} · {r.status}{r.block_status ? ' · ' + r.block_status : ''}</Text>
            ))}
            {sessions?.map(s => (
                <Stack key={s.id} gap={2}>
                    <ActionButton k={k} a={s.action} kind={'resume:' + s.id}/>
                    <Text size="xs" c="dimmed" lineClamp={1} title={s.prompt}>{s.prompt || s.id}</Text>
                </Stack>
            ))}
        </Stack>
    )
}
