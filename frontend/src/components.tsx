import {useEffect, useState} from 'react'
import {ActionIcon, Badge, Box, Button, Card as MCard, Group, Indicator, Menu, Modal, Notification, Paper, Popover, ScrollArea, Select, Stack, Text, Tooltip} from '@mantine/core'
import {IconAppWindow, IconCheck, IconDots, IconMessageSearch, IconPencil, IconSitemap, IconX} from '@tabler/icons-react'
import {AddComment, AppServiceRunning, CheckReviews, MergedPR, Move, PlacedActions, RunAppAction, ShowError, SendQueuedNow, StopAppService, TakeQueuedComment} from '../wailsjs/go/ui/App'
import {store, ui} from '../wailsjs/go/models'
import {Markdown, MarkdownEditor} from './markdown'
import {ALL_STATUSES, STATUS_COLOR, STATUS_LABEL, ago, fmtK, fmtTime, useData, useDraft} from './util'

/** A screen to show; board views may carry a label or repo filter to apply (deep link); epic views a task key for the open modal. */
export type View = {name: string; key?: string; label?: string; repo?: string; task?: string}
export type Nav = (v: View) => void

export const errMsg = (e: unknown) => ShowError(String(e))

let pushToast: (m: string, sticky?: boolean, ms?: number) => void = () => {}
/** Shows a message bottom-right for 10 seconds, or ms (needs <Toasts/> mounted once). A sticky toast stays and has no close button. */
export const toast = (m: string, sticky = false, ms = 10000) => pushToast(m, sticky, ms)

export function Toasts() {
    const [list, setList] = useState<{id: number; m: string; sticky: boolean}[]>([])
    useEffect(() => {
        let n = 0
        const drop = (id: number) => setList(l => l.filter(x => x.id !== id))
        pushToast = (m, sticky = false, ms = 10000) => { const id = ++n; setList(l => [...l, {id, m, sticky}]); if (!sticky) setTimeout(() => drop(id), ms) }
        return () => { pushToast = () => {} }
    }, [])
    return (
        <Stack pos="fixed" bottom={16} right={16} gap="xs" maw={480} style={{zIndex: 1000}}>
            {list.map(x => (
                <Notification key={x.id} withBorder color={x.sticky ? 'red' : undefined} withCloseButton={!x.sticky} onClose={() => setList(l => l.filter(y => y.id !== x.id))}>
                    <Text size="sm" ff="monospace" style={{whiteSpace: 'pre-wrap', wordBreak: 'break-word'}}>{x.m}</Text>
                </Notification>
            ))}
        </Stack>
    )
}

/** An app command failed: the backend already logged it, so only toast. */
const appErr = (e: unknown) => toast(String(e))

/** Buttons for the manifest app actions that asked for `place` (global_nav, task_sidebar, board_card); `k` is the task key ("" for global_nav). */
export function PlacedButtons({place, k = '', size = 'xs'}: {place: string; k?: string; size?: string}) {
    const {data} = useData(() => PlacedActions(place), [place])
    const [busy, setBusy] = useState('')
    const [ask, setAsk] = useState<{label: string; msg: string; go: () => void}>()
    if (!data?.length) return null
    // A manifest action's `confirm` text is asked before it runs (or starts its service).
    const guard = (a: {label: string; confirm?: string}, go: () => void) => (a.confirm ? setAsk({label: a.label, msg: a.confirm, go}) : go())
    const run = (a: {app: string; id: string; label: string; output: boolean; icon?: string; confirm?: string}) => guard(a, () => {
        setBusy(a.app + a.id)
        RunAppAction(a.app, a.id, place, k).then(out => { if (a.output) toast(out || `${a.label}: done`) }, appErr).finally(() => setBusy(''))
    })
    return (
        <Group gap={4} onClick={e => e.stopPropagation()}>
            <Modal opened={!!ask} onClose={() => setAsk(undefined)} title={ask?.label}>
                <Text size="sm" mb="sm">{ask?.msg}</Text>
                <Group justify="flex-end">
                    <Button variant="default" onClick={() => setAsk(undefined)}>Cancel</Button>
                    <Button data-autofocus onClick={() => { ask?.go(); setAsk(undefined) }}>Continue</Button>
                </Group>
            </Modal>
            {/* icon actions first, then text buttons (sort is stable, so manifest order holds within each group) */}
            {[...data].sort((x, y) => Number(!!y.icon) - Number(!!x.icon)).map(a => a.service
                ? <ServiceButton key={a.app + a.id} a={a} place={place} k={k} size={size} guard={guard}/>
                : a.icon
                    ? <Tooltip key={a.app + a.id} label={a.label}><ActionIcon size={iconSize(size)} variant="default" aria-label={a.label} loading={busy === a.app + a.id} onClick={() => run(a)}><ActionImg src={a.icon}/></ActionIcon></Tooltip>
                    : <Button key={a.app + a.id} size={size} variant="default" loading={busy === a.app + a.id} title={a.app} onClick={() => run(a)}>{a.label}</Button>)}
        </Group>
    )
}

/** An action's manifest icon (a data:image/ URL); ActionIcon sizes match the Button sizes they replace. */
const ActionImg = ({src}: {src: string}) => <img src={src} alt="" width={16} height={16}/>
const iconSize = (size: string) => (size.startsWith('compact') ? 'sm' : 'md')

/** A service action at `place` (each place has its own service): a green pulsing dot while its status script succeeds, a click then stops it; otherwise a click starts it and flashes a check/cross. */
function ServiceButton({a, place, k, size, guard}: {a: {app: string; id: string; label: string; icon?: string; confirm?: string}; place: string; k: string; size: string; guard: (a: {label: string; confirm?: string}, go: () => void) => void}) {
    const [running, setRunning] = useState(false)
    const [res, setRes] = useState<boolean | null>(null)
    const check = () => AppServiceRunning(a.app, a.id, place, k).then(setRunning, () => setRunning(false))
    useEffect(() => { check() }, [a.app, a.id, place, k]) // eslint-disable-line react-hooks/exhaustive-deps
    const go = () => (running ? StopAppService(a.app, a.id, place, k) : RunAppAction(a.app, a.id, place, k).then(() => {}))
        .then(() => setRes(true), e => { setRes(false); appErr(e) })
        .finally(() => { setTimeout(() => setRes(null), 1500); check() })
    const click = () => (running ? go() : guard(a, go))
    const flash = res === null ? undefined : res ? <IconCheck size={14} color="var(--mantine-color-green-6)"/> : <IconX size={14} color="var(--mantine-color-red-6)"/>
    return (
        <Tooltip label={running ? `${a.label} running – click to stop` : a.label}>
            <Indicator disabled={!running} color="green" size={8} processing>
                {a.icon
                    ? <ActionIcon size={iconSize(size)} variant="default" aria-label={a.label} onClick={click}>{flash ?? <ActionImg src={a.icon}/>}</ActionIcon>
                    : <Button size={size} variant="default" title={a.app} onClick={click} leftSection={flash}>{a.label}</Button>}
            </Indicator>
        </Tooltip>
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

/** Moves a task; a move that would end running runs (to review or done) asks first. Render the returned modal. */
function useMove(k: string) {
    const [pending, setPending] = useState<{to: string; n: number}>()
    const move = (to: string) => Move(k, to, false).then(n => n && setPending({to, n}), errMsg)
    const confirm = () => { Move(k, pending!.to, true).catch(errMsg); setPending(undefined) }
    const modal = (
        <Modal opened={!!pending} onClose={() => setPending(undefined)} title={`Move ${k} to ${STATUS_LABEL[pending?.to ?? '']}?`}>
            <Text size="sm" mb="sm">{k} has {pending?.n} running run(s). Moving it ends them; the agent stops now.</Text>
            <Group justify="flex-end">
                <Button variant="default" onClick={() => setPending(undefined)}>Cancel</Button>
                <Button data-autofocus color="red" onClick={confirm}>Stop and move</Button>
            </Group>
        </Modal>
    )
    return [move, modal] as const
}

export function StatusSelect({k, status}: {k: string; status: string}) {
    const [move, modal] = useMove(k)
    return <>
        <Select size="xs" w={130} allowDeselect={false} value={status} data={ALL_STATUSES.map(s => ({value: s, label: STATUS_LABEL[s]}))}
                onChange={s => s && move(s)}/>
        {modal}
    </>
}

/** Lightweight status mover for cards. */
function StatusMenu({k, status}: {k: string; status: string}) {
    const [move, modal] = useMove(k)
    return (
        <div onClick={e => e.stopPropagation()}>
            <Menu position="bottom-end" withinPortal>
                <Menu.Target><ActionIcon variant="subtle" color="gray" size="sm" aria-label="Move to"><IconDots size={16}/></ActionIcon></Menu.Target>
                <Menu.Dropdown>
                    <Menu.Label>Move to</Menu.Label>
                    {ALL_STATUSES.filter(s => s !== status).map(s => (
                        <Menu.Item key={s} onClick={() => move(s)}>{STATUS_LABEL[s]}</Menu.Item>
                    ))}
                </Menu.Dropdown>
            </Menu>
            {modal}
        </div>
    )
}

/** `onLabel` adds a clicked label to the board's filters. */
export function Card({c, nav, onLabel}: {c: ui.TaskCard; nav: Nav; onLabel: (l: string) => void}) {
    return (
        <MCard withBorder padding="sm" radius="md" style={{cursor: 'pointer'}} onClick={() => nav({name: 'task', key: c.key})}>
            <Group justify="space-between" wrap="nowrap" gap="xs">
                <Text size="xs" fw={600} c="dimmed">{c.key}{c.parent && <Text span size="xs" fw={400}> ↑ {c.parent}</Text>}</Text>
                <Group gap={4} wrap="nowrap">
                    {c.ctxPeak > 0 && <Badge variant="outline" color="gray" size="xs" title="context peak">{fmtK(c.ctxPeak)}</Badge>}
                    {c.openPR && <Tooltip label="Check PR now (reviews, CI, conflicts)"><ActionIcon variant="subtle" color="gray" size="sm" aria-label="Check PR" onClick={e => { e.stopPropagation(); CheckReviews(c.key).then(() => toast('Checking the PR for reviews, CI and conflicts…', false, 3000), errMsg) }}><IconMessageSearch size={16}/></ActionIcon></Tooltip>}
                    <Tooltip label="Epic view"><ActionIcon variant="subtle" color="gray" size="sm" aria-label="Epic view" onClick={e => { e.stopPropagation(); nav({name: 'epic', key: c.key}) }}><IconSitemap size={16}/></ActionIcon></Tooltip>
                    <StatusMenu k={c.key} status={c.status}/>
                </Group>
            </Group>
            <Text size="sm" fw={500} mt={2}>{c.title}</Text>
            {(c.repos.length > 0 || c.labels.length > 0) && <Group gap={4} mt="xs">
                {c.repos.map(r => <Badge key={r} variant="light" size="xs">{r}</Badge>)}
                {c.labels.map(l => <Badge key={'l:' + l} component="button" variant="outline" color="gray" size="xs" title={'Filter by ' + l}
                                          style={{textTransform: 'none', cursor: 'pointer'}} onClick={e => { e.stopPropagation(); onLabel(l) }}>{l}</Badge>)}
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

/** The "edited" corner badge of a comment the run updated (a question, then its replies); opens the replaced versions. */
function Edits({edits}: {edits: store.CommentEdit[]}) {
    return (
        <Popover width={480} position="bottom-end" withinPortal shadow="md">
            <Popover.Target>
                <Badge size="xs" variant="light" color="gray" ml="auto" style={{cursor: 'pointer'}} component="button">edited {edits.length}×</Badge>
            </Popover.Target>
            <Popover.Dropdown>
                <ScrollArea.Autosize mah={400}>
                    <Stack gap="sm">
                        {[...edits].reverse().map((e, i) => (
                            <Box key={i}>
                                <Text size="xs" c="dimmed">Replaced <Time iso={e.at}/></Text>
                                <Markdown>{e.body}</Markdown>
                            </Box>
                        ))}
                    </Stack>
                </ScrollArea.Autosize>
            </Popover.Dropdown>
        </Popover>
    )
}

/** One comment in the thread. */
// onSendNow: refactor idea: Message owns modal state; if more confirm dialogs appear, adopt @mantine/modals' openConfirmModal.
export function Message({m, onEdit, onSendNow}: {m: store.Comment; onEdit?: () => void; onSendNow?: () => void}) {
    const [asking, setAsking] = useState(false)
    return (
        <Paper withBorder p="sm" radius="md" bg={m.author === 'human' ? 'var(--mantine-color-default-hover)' : undefined}>
            <Group gap="xs" mb={4}>
                <Text size="xs" fw={600}>{m.author}{m.source ? ` (via ${m.source})` : ''}</Text>
                <Time iso={m.created_at}/>
                {onEdit && (onSendNow
                    ? <Tooltip label="Click to send now"><Badge size="xs" variant="light" color="gray" style={{cursor: 'pointer'}} component="button" type="button" onClick={() => setAsking(true)}>queued</Badge></Tooltip>
                    : <Tooltip label="Sent to the agent when the next run starts"><Badge size="xs" variant="light" color="gray">queued</Badge></Tooltip>)}
                {onEdit && <Tooltip label="Move back to the editor"><ActionIcon variant="subtle" color="gray" size="xs" aria-label="Edit queued message" onClick={onEdit}><IconPencil size={12}/></ActionIcon></Tooltip>}
                {!!m.edits?.length && <Edits edits={m.edits}/>}
            </Group>
            <Markdown>{m.body}</Markdown>
            <Modal opened={asking} onClose={() => setAsking(false)} title="Send now?">
                <Text size="sm" mb="sm">Skip the wait and start the agent with this message now.</Text>
                <Group justify="flex-end">
                    <Button variant="default" onClick={() => setAsking(false)}>Cancel</Button>
                    <Button data-autofocus onClick={() => { setAsking(false); onSendNow?.() }}>Send now</Button>
                </Group>
            </Modal>
        </Paper>
    )
}
