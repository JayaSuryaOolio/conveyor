import {useEffect, useRef, useState} from 'react'
import {Anchor, Box, Breadcrumbs, Button, Code, Group, List, Paper, Stack, Text, ThemeIcon, Timeline, Title} from '@mantine/core'
import {IconCheck, IconCircle, IconMessage, IconPlayerPlay} from '@tabler/icons-react'
import {Cancel, Confirm, GetTask, ListTasks, RunLog} from '../wailsjs/go/ui/App'
import {core, store} from '../wailsjs/go/models'
import {Card, CommentBox, Message, Nav, StatusBadge, Time, errMsg} from './components'
import {TaskAgent} from './forms'
import {TaskSidebar} from './workspace'
import {fmtDuration, fmtK, useData} from './util'

const COLUMNS = [['backlog', 'Backlog'], ['todo', 'Todo'], ['running', 'Running'], ['needs_you', 'Needs you'], ['review', 'Review'], ['done', 'Done (7d)']]
const FOCUS_ORDER = ['needs_you', 'review', 'failed', 'running', 'todo']
const WEEK = 7 * 24 * 3600 * 1000

export function Board({nav, q}: {nav: Nav; q: string}) {
    const {data, error} = useData(ListTasks, [])
    if (error) return <Text c="red">{error}</Text>
    const needle = q.toLowerCase()
    const cards = (data ?? []).filter(c => !needle || (c.key + ' ' + c.title).toLowerCase().includes(needle))
    const cols = [...COLUMNS]
    if (cards.some(c => c.status === 'failed')) cols.splice(5, 0, ['failed', 'Failed'])
    return (
        <Box style={{display: 'grid', gridTemplateColumns: `repeat(${cols.length}, minmax(180px, 1fr))`, gap: 'var(--mantine-spacing-sm)', alignItems: 'start', overflowX: 'auto'}}>
            {cols.map(([s, label]) => {
                let list = cards.filter(c => c.status === s)
                if (s === 'done') list = list.filter(c => Date.now() - Date.parse(c.updatedAt) < WEEK)
                return (
                    <Paper key={s} bg="var(--mantine-color-default-hover)" p="xs" radius="md" mih={120}>
                        <Group gap="xs" mb="xs" px={4}>
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
                        {c.lastSummary && <Text size="xs" c="dimmed" lineClamp={2}>{c.lastSummary}</Text>}
                        <CommentBox k={c.key} onDone={reload}/>
                    </Stack>
                </Paper>
            ))}
        </Stack>
    )
}

type Item = {t: string; run?: store.Run; comment?: store.Comment}

function Criteria({text}: {text: string}) {
    const lines = text.split('\n').map(l => l.trim()).filter(l => /^[-*]\s*\[[ xX]\]/.test(l))
    if (!lines.length) return null
    return (
        <List spacing="xs" size="sm" center>
            {lines.map((l, i) => {
                const done = /\[[xX]\]/.test(l)
                return (
                    <List.Item key={i} icon={<ThemeIcon size={20} radius="xl" variant={done ? 'filled' : 'default'} color="green">{done ? <IconCheck size={12}/> : <IconCircle size={10}/>}</ThemeIcon>}>
                        {l.replace(/^[-*]\s*\[[ xX]\]\s*/, '')}
                    </List.Item>
                )
            })}
        </List>
    )
}

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
                            : <Text key={i} size="sm" fw={e.kind === 'user' ? 600 : undefined} style={{whiteSpace: 'pre-wrap', wordBreak: 'break-word'}}>{e.text}</Text>
                    ))}
                </Paper>
            )}
        </Stack>
    )
}

export function TaskPage({k, nav}: {k: string; nav: Nav}) {
    const {data: d, error, reload} = useData(() => GetTask(k), [k])
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
        <Box style={{display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) 300px', gap: 'var(--mantine-spacing-xl)', alignItems: 'start'}}>
            <Stack>
                <Breadcrumbs>
                    <Anchor size="sm" component="button" onClick={() => nav({name: 'board'})}>Board</Anchor>
                    <Text size="sm">{t.key}</Text>
                </Breadcrumbs>
                <Title order={3}>{t.title}</Title>
                <Group>
                    <TaskAgent k={t.key} repos={t.repos?.map(r => r.repo_name) ?? []} agent={t.agent ?? ''}/>
                    <Button variant="default" color="red" size="xs" onClick={() => Confirm('Cancel this task?').then(ok => { if (ok) return Cancel(t.key) }).catch(errMsg)}>Cancel task</Button>
                </Group>
                <Paper withBorder p="md" radius="md">
                    <Text size="sm" c={t.description ? undefined : 'dimmed'} style={{whiteSpace: 'pre-wrap', wordBreak: 'break-word'}}>{t.description || '(no description)'}</Text>
                </Paper>
                {latest?.criteria && <><Title order={5}>Acceptance criteria</Title><Criteria text={latest.criteria}/></>}
                <Title order={5}>Thread</Title>
                <Timeline bulletSize={24} lineWidth={2}>
                    {items.map(it => it.run ? (
                        <Timeline.Item key={'r' + it.run.id} bullet={<IconPlayerPlay size={12}/>}
                                       title={`Run ${runNo.get(it.run.id)} · ${it.run.mode}`}>
                            <Text size="xs" c="dimmed">
                                {it.run.api_calls} calls · {fmtK(it.run.ctx_peak)} peak · {fmtDuration(it.run.started_at, it.run.ended_at)} · <Time iso={it.run.started_at}/>
                            </Text>
                            <SessionLog run={it.run}/>
                        </Timeline.Item>
                    ) : (
                        <Timeline.Item key={'c' + it.comment!.id} bullet={<IconMessage size={12}/>}>
                            <Message m={it.comment!}/>
                        </Timeline.Item>
                    ))}
                </Timeline>
                <CommentBox k={t.key} onDone={reload}/>
            </Stack>
            <TaskSidebar t={t} runs={d.runs} reload={reload}/>
        </Box>
    )
}
