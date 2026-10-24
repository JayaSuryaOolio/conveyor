import {lazy, Suspense, useEffect, useRef, useState} from 'react'
import {ActionIcon, Anchor, Box, Breadcrumbs, Button, Code, Group, Paper, Stack, Text, Timeline, Title} from '@mantine/core'
import {useLocalStorage} from '@mantine/hooks'
import {IconArrowDown, IconArrowUp, IconLayoutSidebarRightCollapse, IconLayoutSidebarRightExpand, IconMessage, IconPlayerPlay} from '@tabler/icons-react'
import {GetTask, ListTasks, RunLog} from '../wailsjs/go/ui/App'
import {core, store, ui} from '../wailsjs/go/models'
import {Card, CommentBox, Message, Nav, StatusBadge, Time, errMsg} from './components'
import {Markdown} from './markdown'
import {Relations} from './relations'
import {TaskSidebar} from './workspace'
import {fmtDuration, fmtK, useData} from './util'

const TermDock = lazy(() => import('./terminals'))

const COLUMNS = [['backlog', 'Backlog'], ['todo', 'Todo'], ['running', 'Running'], ['needs_you', 'Needs you'], ['review', 'Review'], ['done', 'Done (7d)'], ['cancelled', 'Cancelled (7d)']]
const FOCUS_ORDER = ['needs_you', 'review', 'failed', 'running', 'todo']
const WEEK = 7 * 24 * 3600 * 1000
const ARCHIVE = ['done', 'cancelled'] // collapsed until opened

export function Board({nav, q}: {nav: Nav; q: string}) {
    const {data, error} = useData(ListTasks, [])
    const [open, setOpen] = useState<string[]>([])
    if (error) return <Text c="red">{error}</Text>
    const shut = (s: string) => ARCHIVE.includes(s) && !open.includes(s)
    const toggle = (s: string) => setOpen(o => o.includes(s) ? o.filter(x => x !== s) : [...o, s])
    const needle = q.toLowerCase()
    const cards = (data ?? []).filter(c => !needle || (c.key + ' ' + c.title).toLowerCase().includes(needle))
    const cols = [...COLUMNS]
    if (cards.some(c => c.status === 'failed')) cols.splice(5, 0, ['failed', 'Failed'])
    return (
        <Box style={{display: 'grid', gridTemplateColumns: cols.map(([s]) => shut(s) ? '36px' : 'minmax(180px, 1fr)').join(' '), gap: 'var(--mantine-spacing-sm)', alignItems: 'start', overflowX: 'auto'}}>
            {cols.map(([s, label]) => {
                let list = cards.filter(c => c.status === s)
                if (ARCHIVE.includes(s)) list = list.filter(c => Date.now() - Date.parse(c.updatedAt) < WEEK)
                if (shut(s)) return (
                    <Paper key={s} bg="var(--mantine-color-default-hover)" radius="md" mih={120} title={'Show ' + label}
                           style={{cursor: 'pointer', writingMode: 'vertical-rl', padding: '12px 0', display: 'flex', alignItems: 'center', gap: 6}} onClick={() => toggle(s)}>
                        <Text size="sm" fw={600}>{label}</Text>
                        <Text size="xs" c="dimmed">{list.length}</Text>
                    </Paper>
                )
                return (
                    <Paper key={s} bg="var(--mantine-color-default-hover)" p="xs" radius="md" mih={120}>
                        <Group gap="xs" mb="xs" px={4} style={ARCHIVE.includes(s) ? {cursor: 'pointer'} : undefined}
                               title={ARCHIVE.includes(s) ? 'Collapse' : undefined} onClick={ARCHIVE.includes(s) ? () => toggle(s) : undefined}>
                            <Text size="sm" fw={600}>{label}</Text>
                            <Text size="xs" c="dimmed">{list.length}</Text>
                        </Group>
                        <Stack gap="xs">{list.map(c => <Card key={c.key} c={c} nav={nav}/>)}</Stack>
                    </Paper>
                )
            })}
        </Box>
    )
}

export function Focus({nav, q}: {nav: Nav; q: string}) {
    const {data, error, reload} = useData(ListTasks, [])
    if (error) return <Text c="red">{error}</Text>
    const needle = q.toLowerCase()
    const rows = (data ?? [])
        .filter(c => FOCUS_ORDER.includes(c.status) && (!needle || (c.key + ' ' + c.title).toLowerCase().includes(needle)))
        .sort((a, b) => FOCUS_ORDER.indexOf(a.status) - FOCUS_ORDER.indexOf(b.status))
    return (
        <Stack maw={800}>
            <Title order={3}>Focus</Title>
            {rows.length === 0 && <Text c="dimmed">Nothing needs you.</Text>}
            {rows.map(c => (
                <Paper key={c.key} withBorder p="md" radius="md">
                    <Stack gap="xs">
                        <Group wrap="nowrap">
                            <StatusBadge status={c.status}/>
                            <Text size="sm" fw={600}>{c.key}</Text>
                            <Text size="sm" style={{flex: 1}} lineClamp={1}>{c.title}</Text>
                            <Button variant="default" size="xs" onClick={() => nav({name: 'task', key: c.key})}>Open</Button>
                        </Group>
                        {c.lastSummary && <Box mah={40} style={{overflow: 'hidden'}}><Markdown c="dimmed">{c.lastSummary}</Markdown></Box>}
                        <CommentBox k={c.key} onDone={reload}/>
                    </Stack>
                </Paper>
            ))}
        </Stack>
    )
}

type Item = {t: string; run?: store.Run; comment?: store.Comment}

const Criteria = ({text}: {text: string}) => <Markdown>{text}</Markdown>

const secs = (n: number) => (n < 60 ? n + 's' : Math.floor(n / 60) + 'm ' + (n % 60) + 's')

/** Chat log of a run's Claude session: polls while the run is active, once on demand after. */
function SessionLog({run}: {run: store.Run}) {
    const active = run.status === 'launching' || run.status === 'running'
    const [open, setOpen] = useState(false)
    const [entries, setEntries] = useState<core.RunLog['entries']>([])
    const [err, setErr] = useState('')
    const [now, setNow] = useState(Date.now())
    const after = useRef(0)
    const box = useRef<HTMLDivElement>(null)
    const show = active || open
    useEffect(() => {
        if (!show) return
        let stop = false
        const poll = () => RunLog(run.id, after.current).then(l => {
            if (stop) return
            after.current = l.next
            setErr('')
            if (l.entries.length) setEntries(e => [...e, ...l.entries])
        }, e => { if (!stop) setErr(String(e)) })
        poll()
        if (!active) return () => { stop = true }
        const p = setInterval(poll, 2000), c = setInterval(() => setNow(Date.now()), 1000)
        return () => { stop = true; clearInterval(p); clearInterval(c) }
    }, [run.id, show, active])
    useEffect(() => { box.current?.scrollTo({top: box.current.scrollHeight}) }, [entries])
    const lastTool = [...entries].reverse().find(e => e.kind === 'tool')
    return (
        <Stack gap={6} mt="xs">
            {active ? (
                <Group gap="xs" wrap="nowrap">
                    <span className="pulse"/>
                    <Text size="sm">Working · {secs(Math.max(0, Math.round((now - Date.parse(run.started_at)) / 1000)))}</Text>
                    {lastTool && <Text size="xs" c="dimmed" lineClamp={1}>· {lastTool.text}</Text>}
                </Group>
            ) : (
                <Button variant="subtle" size="compact-xs" w="fit-content" onClick={() => setOpen(!open)}>{open ? 'Hide session' : 'Show session'}</Button>
            )}
            {err && <Text size="xs" c="dimmed">{err}</Text>}
            {show && entries.length > 0 && (
                <Paper withBorder p="xs" radius="md" mah={320} style={{overflowY: 'auto'}} ref={box}>
                    {entries.map((e, i) => (
                        e.kind === 'tool'
                            ? <Code key={i} block bg="transparent" c="dimmed" style={{whiteSpace: 'pre-wrap', wordBreak: 'break-word'}}>{e.text}</Code>
                            : e.kind === 'user'
                                ? <Text key={i} size="sm" fw={600} style={{whiteSpace: 'pre-wrap', wordBreak: 'break-word'}}>{e.text}</Text>
                                : <Markdown key={i}>{e.text}</Markdown>
                    ))}
                </Paper>
            )}
        </Stack>
    )
}

export function TaskPage({k, nav}: {k: string; nav: Nav}) {
    const {data: d, error, reload} = useData(() => GetTask(k), [k])
    const [added, setAdded] = useState<ui.TermInfo>()
    const [side, setSide] = useLocalStorage({key: 'task-sidebar', defaultValue: true})
    if (error) return <Text c="red">{error}</Text>
    if (!d) return <Text c="dimmed">Loading…</Text>
    const t = d.task
    const items: Item[] = [
        ...d.runs.map((run): Item => ({t: run.started_at, run})),
        ...d.comments.map((comment): Item => ({t: comment.created_at, comment})),
    ].sort((a, b) => (a.t < b.t ? -1 : a.t > b.t ? 1 : a.run ? -1 : 1))
    const runNo = new Map(d.runs.map((r, i) => [r.id, i + 1]))
    const latest = d.runs[d.runs.length - 1]
    return (
        // Only the thread scrolls: the comment box, sidebar and terminal dock stay put.
        <Box style={{display: 'flex', flexDirection: 'column', height: 'calc(100vh - 2 * var(--mantine-spacing-md))'}}>
        <Box style={{flex: 1, minHeight: 0, display: 'grid', gridTemplateColumns: `minmax(0, 1fr) ${side ? '300px' : 'auto'}`, gap: side ? 'var(--mantine-spacing-xl)' : 'var(--mantine-spacing-xs)'}}>
            <Stack gap="sm" mih={0}>
                <ScrollBox>
                    <Stack>
                        <Breadcrumbs>
                            <Anchor size="sm" component="button" onClick={() => nav({name: 'board'})}>Board</Anchor>
                            <Text size="sm">{t.key}</Text>
                        </Breadcrumbs>
                        <Title order={3}>{t.title}</Title>
                        <Paper withBorder p="md" radius="md">
                            <Markdown c={t.description ? undefined : 'dimmed'}>{t.description || '(no description)'}</Markdown>
                        </Paper>
                        <Relations k={t.key} status={t.status} rel={d.relations} nav={nav} reload={reload}/>
                        {latest?.criteria && <><Title order={5}>Acceptance criteria</Title><Criteria text={latest.criteria}/></>}
                        <Title order={5}>Thread</Title>
                        <Timeline bulletSize={24} lineWidth={2}>
                            {items.map(it => it.run ? (
                                <Timeline.Item key={'r' + it.run.id} id={'run-' + it.run.id} bullet={<IconPlayerPlay size={12}/>}
                                               title={`Run ${runNo.get(it.run.id)} · ${it.run.mode}`}>
                                    <Text size="xs" c="dimmed">
                                        {it.run.api_calls} calls · {fmtK(it.run.ctx_peak)} peak · {fmtDuration(it.run.started_at, it.run.ended_at)} · <Time iso={it.run.started_at}/>
                                    </Text>
                                    <SessionLog run={it.run}/>
                                </Timeline.Item>
                            ) : (
                                <Timeline.Item key={'c' + it.comment!.id} bullet={<IconMessage size={12}/>}>
                                    <Message m={it.comment!} queued={t.status === 'running' && it.comment!.author === 'human' && !it.comment!.consumed_by_run_id}/>
                                </Timeline.Item>
                            ))}
                        </Timeline>
                    </Stack>
                </ScrollBox>
                <Box style={{flex: 'none'}}><CommentBox k={t.key} onDone={reload}/></Box>
            </Stack>
            <Box mih={0} style={{overflowY: 'auto'}}>
                <Group justify="flex-end" mb={4}>
                    <ActionIcon variant="subtle" color="gray" aria-label={side ? 'Hide details' : 'Show details'} title={side ? 'Hide details' : 'Show details'} onClick={() => setSide(!side)}>
                        {side ? <IconLayoutSidebarRightCollapse size={18}/> : <IconLayoutSidebarRightExpand size={18}/>}
                    </ActionIcon>
                </Group>
                {side && <TaskSidebar t={t} runs={d.runs} reload={reload} onTerm={setAdded}/>}
            </Box>
        </Box>
        <Box mx="calc(-1 * var(--mantine-spacing-md))" mb="calc(-1 * var(--mantine-spacing-md))" mt="md" style={{flex: 'none'}}>
            <Suspense fallback={null}><TermDock k={t.key} added={added}/></Suspense>
        </Box>
        </Box>
    )
}

/** Scrolling column with floating jump-to-top / jump-to-bottom buttons, shown only when there is somewhere to go. */
function ScrollBox({children}: {children: React.ReactNode}) {
    const el = useRef<HTMLDivElement>(null)
    const [pos, setPos] = useState({top: true, bottom: true})
    const update = () => {
        const e = el.current
        if (e) setPos({top: e.scrollTop < 40, bottom: e.scrollHeight - e.scrollTop - e.clientHeight < 40})
    }
    useEffect(() => {
        const ro = new ResizeObserver(update)
        if (el.current) { ro.observe(el.current); if (el.current.firstElementChild) ro.observe(el.current.firstElementChild) }
        return () => ro.disconnect()
    }, [])
    const go = (top: number) => el.current?.scrollTo({top, behavior: 'smooth'})
    return (
        <Box pos="relative" style={{flex: 1, minHeight: 0}}>
            <Box ref={el} onScroll={update} h="100%" pr="xs" style={{overflowY: 'auto'}}>{children}</Box>
            <Stack gap={4} pos="absolute" right={16} bottom={8}>
                {!pos.top && <ActionIcon variant="default" radius="xl" size="lg" aria-label="Scroll to top" onClick={() => go(0)}><IconArrowUp size={16}/></ActionIcon>}
                {!pos.bottom && <ActionIcon variant="default" radius="xl" size="lg" aria-label="Scroll to bottom" onClick={() => go(el.current!.scrollHeight)}><IconArrowDown size={16}/></ActionIcon>}
            </Stack>
        </Box>
    )
}
