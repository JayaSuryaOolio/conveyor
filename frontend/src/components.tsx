import {useEffect, useState} from 'react'
import {ActionIcon, Badge, Box, Button, Card as MCard, Group, Menu, Modal, Notification, Paper, Select, Stack, Text, Tooltip} from '@mantine/core'
import {IconAppWindow, IconDots, IconPencil} from '@tabler/icons-react'
import {AddComment, MergedPR, Move, PlacedActions, RunAppAction, ShowError, TakeQueuedComment} from '../wailsjs/go/ui/App'
import {store, ui} from '../wailsjs/go/models'
import {Markdown, MarkdownEditor} from './markdown'
import {ALL_STATUSES, STATUS_COLOR, STATUS_LABEL, ago, fmtK, fmtTime, useData, useDraft} from './util'

export type Nav = (v: {name: string; key?: string}) => void

export const errMsg = (e: unknown) => ShowError(String(e))

let pushToast: (m: string) => void = () => {}
/** Shows a message bottom-right for 10 seconds (needs <Toasts/> mounted once). */
export const toast = (m: string) => pushToast(m)

export function Toasts() {
    const [list, setList] = useState<{id: number; m: string}[]>([])
    useEffect(() => {
        let n = 0
        const drop = (id: number) => setList(l => l.filter(x => x.id !== id))
        pushToast = m => { const id = ++n; setList(l => [...l, {id, m}]); setTimeout(() => drop(id), 10000) }
        return () => { pushToast = () => {} }
    }, [])
    return (
        <Stack pos="fixed" bottom={16} right={16} gap="xs" maw={480} style={{zIndex: 1000}}>
            {list.map(x => (
                <Notification key={x.id} withBorder onClose={() => setList(l => l.filter(y => y.id !== x.id))}>
                    <Text size="sm" ff="monospace" style={{whiteSpace: 'pre-wrap', wordBreak: 'break-word'}}>{x.m}</Text>
                </Notification>
            ))}
        </Stack>
    )
}

/** Runs an app; an output app's result (or "done") is toasted. */
export const runApp = (a: store.App, p: Promise<string>) =>
    p.then(out => { if (a.output) toast(out || `${a.name}: done`) }, errMsg)

/** Buttons for the manifest app actions that asked for `place` (global_nav, task_sidebar, board_card); `k` is the task key ("" for global_nav). */
export function PlacedButtons({place, k = '', size = 'xs'}: {place: string; k?: string; size?: string}) {
    const {data} = useData(() => PlacedActions(place), [place])
    const [busy, setBusy] = useState('')
    if (!data?.length) return null
    const run = (a: {app: string; id: string; label: string; output: boolean}) => {
        setBusy(a.app + a.id)
        RunAppAction(a.app, a.id, k).then(out => { if (a.output) toast(out || `${a.label}: done`) }, errMsg).finally(() => setBusy(''))
    }
    return (
        <Group gap={4} onClick={e => e.stopPropagation()}>
            {data.map(a => <Button key={a.app + a.id} size={size} variant="default" loading={busy === a.app + a.id} title={a.app} onClick={() => run(a)}>{a.label}</Button>)}
        </Group>
    )
}

/** The app's macOS icon (from AppIcons), else a generic window glyph. */
export const AppIcon = ({name, icons, size = 18}: {name: string; icons?: Record<string, string>; size?: number}) =>
    icons?.[name] ? <img src={icons[name]} alt="" width={size} height={size}/> : <IconAppWindow size={size}/>

/** Relative time with the absolute one on hover. */
export const Time = ({iso}: {iso: string}) => (
    <Tooltip label={fmtTime(iso)}><Text span size="xs" c="dimmed">{ago(iso)}</Text></Tooltip>
)

export const StatusBadge = ({status}: {status: string}) => (
    <Badge variant="light" color={STATUS_COLOR[status]} size="sm">{STATUS_LABEL[status] ?? status}</Badge>
)

export function StatusSelect({k, status}: {k: string; status: string}) {
    return (
        <Select size="xs" w={130} allowDeselect={false} value={status} data={ALL_STATUSES.map(s => ({value: s, label: STATUS_LABEL[s]}))}
                onChange={s => s && Move(k, s).catch(errMsg)}/>
    )
}

/** Lightweight status mover for cards. */
function StatusMenu({k, status}: {k: string; status: string}) {
    return (
        <div onClick={e => e.stopPropagation()}>
            <Menu position="bottom-end" withinPortal>
                <Menu.Target><ActionIcon variant="subtle" color="gray" size="sm" aria-label="Move to"><IconDots size={16}/></ActionIcon></Menu.Target>
                <Menu.Dropdown>
                    <Menu.Label>Move to</Menu.Label>
                    {ALL_STATUSES.filter(s => s !== status).map(s => (
                        <Menu.Item key={s} onClick={() => Move(k, s).catch(errMsg)}>{STATUS_LABEL[s]}</Menu.Item>
                    ))}
                </Menu.Dropdown>
            </Menu>
        </div>
    )
}

export function Card({c, nav}: {c: ui.TaskCard; nav: Nav}) {
    return (
        <MCard withBorder padding="sm" radius="md" style={{cursor: 'pointer'}} onClick={() => nav({name: 'task', key: c.key})}>
            <Group justify="space-between" wrap="nowrap" gap="xs">
                <Text size="xs" fw={600} c="dimmed">{c.key}{c.parent && <Text span size="xs" fw={400}> ↑ {c.parent}</Text>}</Text>
                <Group gap={4} wrap="nowrap">
                    {c.ctxPeak > 0 && <Badge variant="outline" color="gray" size="xs" title="context peak">{fmtK(c.ctxPeak)}</Badge>}
                    <StatusMenu k={c.key} status={c.status}/>
                </Group>
            </Group>
            <Text size="sm" fw={500} mt={2}>{c.title}</Text>
            {(c.repos.length > 0 || c.labels.length > 0) && <Group gap={4} mt="xs">
                {c.repos.map(r => <Badge key={r} variant="light" size="xs">{r}</Badge>)}
                {c.labels.map(l => <Badge key={'l:' + l} variant="outline" color="gray" size="xs" style={{textTransform: 'none'}}>{l}</Badge>)}
            </Group>}
            {c.lastSummary && <Box mt="xs" mah={40} style={{overflow: 'hidden'}}><Markdown c="dimmed">{c.lastSummary}</Markdown></Box>}
            {c.repos.length > 0 && <Box mt="xs"><PlacedButtons place="board_card" k={c.key} size="compact-xs"/></Box>}
        </MCard>
    )
}

/** `restored` (a fresh object per request) appends a pulled-back queued message to the editor. */
export function CommentBox({k, placeholder = 'Comment…', onDone, restored}: {k: string; placeholder?: string; onDone?: () => void; restored?: {body: string}}) {
    const [text, setText] = useDraft('comment:' + k)
    useEffect(() => { if (restored) setText(t => t ? t + '\n\n' + restored.body : restored.body) }, [restored])
    const [busy, setBusy] = useState(false)
    const [merged, setMerged] = useState('')
    const post = (after: string) => {
        setMerged('')
        setBusy(true)
        AddComment(k, text, after).then(() => { setText(''); onDone?.() }, errMsg).finally(() => setBusy(false))
    }
    // Once a PR is merged, a comment no longer simply reopens the task: ask what it is for.
    const send = () => {
        if (!text.trim() || busy) return
        MergedPR(k).then(pr => pr ? setMerged(pr) : post(''), errMsg)
    }
    return <>
        <MarkdownEditor value={text} onChange={setText} onSubmit={send} submitting={busy} canSubmit={!!text.trim()} attachKey={k} placeholder={placeholder}/>
        <Modal opened={!!merged} onClose={() => setMerged('')} title={`${k}'s PR is merged`}>
            <Stack gap="sm">
                <Text size="sm">{merged} is merged, so {k} can't take more changes on its branch: a task keeps one branch and one PR per repo. What is this comment for?</Text>
                <Button data-autofocus onClick={() => post('')}>More work: start a follow-up sub-task</Button>
                <Button variant="default" onClick={() => post('ask')}>A question: answer it, no code changes</Button>
                <Button variant="subtle" color="gray" onClick={() => post('note')}>Just a note: nothing runs</Button>
            </Stack>
        </Modal>
    </>
}

/** One comment in the thread. */
export function Message({m, onEdit}: {m: store.Comment; onEdit?: () => void}) {
    return (
        <Paper withBorder p="sm" radius="md" bg={m.author === 'human' ? 'var(--mantine-color-default-hover)' : undefined}>
            <Group gap="xs" mb={4}>
                <Text size="xs" fw={600}>{m.author}</Text>
                <Time iso={m.created_at}/>
                {onEdit && <>
                    <Tooltip label="Sent to the agent when the current run stops"><Badge size="xs" variant="light" color="gray">queued</Badge></Tooltip>
                    <Tooltip label="Move back to the editor"><ActionIcon variant="subtle" color="gray" size="xs" aria-label="Edit queued message" onClick={onEdit}><IconPencil size={12}/></ActionIcon></Tooltip>
                </>}
            </Group>
            <Markdown>{m.body}</Markdown>
        </Paper>
    )
}
