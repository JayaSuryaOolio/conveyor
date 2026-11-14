import {createContext, useContext, useEffect, useMemo, useRef, useState} from 'react'
import {ActionIcon, Alert, Anchor, Badge, Box, Button, Group, Indicator, Menu, Modal, Paper, ScrollArea, SegmentedControl, SimpleGrid, Stack, Table, Text, Textarea, TextInput, Tooltip} from '@mantine/core'
import {useLocalStorage} from '@mantine/hooks'
import {IconCode, IconExternalLink, IconGitPullRequest, IconMessageQuestion, IconPlayerPlay, IconPlayerStop, IconPlus, IconTerminal2} from '@tabler/icons-react'
import {Background, Controls, Handle, MarkerType, Position, ReactFlow, useEdgesState, useNodesState, type Connection, type Edge, type Node, type NodeProps} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import {CreateTask, EpicApprove, EpicGraph, EpicRestart, InstallExample, ListApps, Move, OpenApp, RunAction, Stop, Link, Unlink} from '../wailsjs/go/ui/App'
import {BrowserOpenURL} from '../wailsjs/runtime/runtime'
import {core, store} from '../wailsjs/go/models'
import {Nav, StatusBadge, errMsg} from './components'
import {CARD, columns, layout, trace} from './epicLayout'
import {STATUS_COLOR, useData} from './util'
import {JiraSource} from './jiraSource'
import {TaskPreview} from './views'

const STAGES = ['chart', 'decide', 'spec', 'build', 'verify', 'done']
const KIND_COLOR: Record<string, string> = {grilling: 'orange', prototype: 'grape', research: 'teal', build: 'blue'}
const NO_RUN = new Set(['running', 'done'])
const FINISHED = new Set(['done', 'cancelled'])
const READY = new Set(['backlog', 'todo', 'failed'])
const c6 = (color: string) => `var(--mantine-color-${color}-6)`

type Ctx = {nav: Nav; addSub: (parent: string) => void; open: (key: string) => void; lit: Set<string> | null; hover: (key: string) => void}
const Ctx = createContext<Ctx>({nav: () => {}, addSub: () => {}, open: () => {}, lit: null, hover: () => {}})
type CardData = {n: core.GraphNode; prs: store.FlowPR[]}

const skillOf = (labels: string[]) => labels.filter(l => l.startsWith('skill:')).map(l => l.slice(6))

/** Edge colour says whether the link still holds its target back: done blocker = satisfied, running = in progress, else waiting. */
function edgeState(l: store.Link, status: (k: string) => string): {color: string; label: string; animated?: boolean} {
    if (l.kind !== 'blocks') return {color: 'gray', label: 'relates'}
    const s = status(l.from)
    if (FINISHED.has(s)) return {color: 'green', label: 'unblocked'}
    if (s === 'running') return {color: 'cyan', label: 'blocks · running', animated: true}
    return {color: 'orange', label: 'blocks'}
}

/** Leaf tasks only: groups are containers, not work. */
const leavesOf = (g: core.Graph) => g.nodes.filter(n => !g.nodes.some(c => c.parent === n.key))

function PrChip({p}: {p: store.FlowPR}) {
    const bits = [p.review, p.checks, p.unresolved ? `${p.unresolved} unresolved` : ''].filter(Boolean) // blank until the review check has data (sub-task A)
    return (
        <Tooltip label={p.pr_url}>
            <Badge component="button" className="nodrag" size="xs" variant="light" color="violet" leftSection={<IconGitPullRequest size={10}/>}
                   style={{cursor: 'pointer', textTransform: 'none'}} onClick={e => { e.stopPropagation(); p.pr_url && BrowserOpenURL(p.pr_url) }}>
                {p.repo}{bits.length > 0 && ' · ' + bits.join(' · ')}
            </Badge>
        </Tooltip>
    )
}

/** Compact card: header icons + title + one line of read-only details. Clicking the card or any detail opens the task in a modal, where agent/model/skills/kind are edited. */
function TaskNode({data}: NodeProps<Node<CardData>>) {
    const {n, prs} = data
    const {nav, open, lit, hover} = useContext(Ctx)
    const running = n.status === 'running'
    const [editors, setEditors] = useState<store.App[]>([])
    const stuck = n.status === 'needs_you'
    const skills = skillOf(n.labels)
    // Header controls get `nodrag nowheel` so React Flow doesn't start a node drag or zoom the canvas, and stop clicks reaching the card's open-modal handler.
    const nd = 'nodrag nowheel'
    const stop = (e: React.MouseEvent) => e.stopPropagation()
    const details = [
        n.kind && <Badge key="kind" size="xs" color={KIND_COLOR[n.kind] ?? 'gray'} variant="filled">{n.kind}</Badge>,
        n.agent && <Text key="agent" size="xs" c="dimmed">{n.agent}</Text>,
        n.model && <Text key="model" size="xs" c="dimmed">{n.model}</Text>,
        skills.length > 0 && <Tooltip key="skills" label={skills.join(', ')}><Text size="xs" c="dimmed">{skills.length} skill{skills.length === 1 ? '' : 's'}</Text></Tooltip>,
        ...n.repos.map(r => <Badge key={r} size="xs" variant="light">{r}</Badge>),
        n.worktrees.length > 0 && <Tooltip key="wt" label={'worktree: ' + n.worktrees.join(', ')}><Badge size="xs" variant="outline" color="gray">worktree</Badge></Tooltip>,
    ].filter(Boolean)
    return (
        <Paper withBorder radius="md" p="xs" w={CARD.w} shadow="sm" onClick={() => open(n.key)} onMouseEnter={() => hover(n.key)} onMouseLeave={() => hover('')}
               style={{cursor: 'pointer', borderLeft: `4px solid ${c6(KIND_COLOR[n.kind] ?? 'gray')}`,
                       opacity: lit && !lit.has(n.key) ? 0.3 : 1, transition: 'opacity 120ms',
                       outline: lit?.has(n.key) ? `1px solid ${c6('blue')}` : undefined}}>
            <Handle type="target" position={Position.Left}/>
            <Handle type="source" position={Position.Right}/>
            <Stack gap={6}>
                <Group justify="space-between" wrap="nowrap" gap="xs">
                    <Group gap={6} wrap="nowrap">
                        <Text size="xs" fw={600} c="dimmed">{n.key}</Text>
                        <Indicator disabled={!running} processing color="cyan" size={8} offset={-2}><StatusBadge status={n.status}/></Indicator>
                    </Group>
                    <Group gap={2} wrap="nowrap" className={nd} onClick={stop}>
                        <Tooltip label="Open task page"><ActionIcon size="sm" variant="subtle" aria-label="Open task" onClick={() => nav({name: 'task', key: n.key})}><IconExternalLink size={14}/></ActionIcon></Tooltip>
                        {running
                            ? <Tooltip label="Stop the run"><ActionIcon size="sm" variant="subtle" color="red" aria-label="Stop" onClick={() => Stop(n.key).catch(errMsg)}><IconPlayerStop size={14}/></ActionIcon></Tooltip>
                            : !NO_RUN.has(n.status) && <Tooltip label={n.status === 'todo' ? 'Queued' : n.status === 'backlog' ? 'Run' : 'Requeue'}><ActionIcon size="sm" variant="subtle" aria-label="Run" disabled={n.status === 'todo'} onClick={() => Move(n.key, 'todo', false).catch(errMsg)}><IconPlayerPlay size={14}/></ActionIcon></Tooltip>}
                        {stuck && <Tooltip label="Answer"><ActionIcon size="sm" variant="subtle" color="orange" aria-label="Answer" onClick={() => open(n.key)}><IconMessageQuestion size={14}/></ActionIcon></Tooltip>}
                        {prs[0]?.pr_url && <Tooltip label="Open PR"><ActionIcon size="sm" variant="subtle" aria-label="Open PR" onClick={() => BrowserOpenURL(prs[0].pr_url!)}><IconGitPullRequest size={14}/></ActionIcon></Tooltip>}
                        {n.worktrees.length > 0 && <>
                            <Tooltip label="Shell in worktree"><ActionIcon size="sm" variant="subtle" aria-label="Shell" onClick={() => RunAction(n.key, 'shell', n.worktrees[0]).catch(errMsg)}><IconTerminal2 size={14}/></ActionIcon></Tooltip>
                            <Menu withinPortal onOpen={() => ListApps().then(a => setEditors(a ?? []), errMsg)}>
                                <Menu.Target><Tooltip label="Open in…"><ActionIcon size="sm" variant="subtle" aria-label="Open in editor"><IconCode size={14}/></ActionIcon></Tooltip></Menu.Target>
                                <Menu.Dropdown>{editors.map(a => <Menu.Item key={a.name} onClick={() => OpenApp(a.name, n.key, n.worktrees[0]).catch(errMsg)}>{a.name}</Menu.Item>)}</Menu.Dropdown>
                            </Menu>
                        </>}
                    </Group>
                </Group>
                <Text size="sm" fw={500} lineClamp={2}>{n.title}</Text>
                <Group gap={6} wrap="wrap">
                    {details}
                    {prs.filter(p => p.pr_url).map(p => <PrChip key={p.repo} p={p}/>)}
                </Group>
            </Stack>
        </Paper>
    )
}

function GroupNode({data}: NodeProps<Node<{n: core.GraphNode}>>) {
    const {nav, addSub} = useContext(Ctx)
    const n = data.n
    return (
        <Box w="100%" h="100%" style={{border: '1px dashed var(--mantine-color-default-border)', borderRadius: 12, background: 'var(--mantine-color-default-hover)'}}>
            <Handle type="target" position={Position.Left}/>
            <Handle type="source" position={Position.Right}/>
            <Group px="sm" py={8} justify="space-between" wrap="nowrap">
                <Group gap={6} wrap="nowrap" style={{minWidth: 0}}>
                    <Anchor component="button" size="xs" fw={600} onClick={() => nav({name: 'task', key: n.key})}>{n.key}</Anchor>
                    <Text size="xs" truncate>{n.title}</Text>
                    <StatusBadge status={n.status}/>
                </Group>
                <Tooltip label="Add sub-task"><ActionIcon className="nodrag" size="sm" variant="light" aria-label="Add sub-task" onClick={() => addSub(n.key)}><IconPlus size={14}/></ActionIcon></Tooltip>
            </Group>
        </Box>
    )
}

const nodeTypes = {task: TaskNode, parent: GroupNode} // not `group`: that is React Flow's built-in type and would add its own box

function Header({g, nav, root}: {g: core.Graph; nav: Nav; root: core.GraphNode}) {
    const kids = g.nodes.filter(n => n.key !== root.key && !g.nodes.some(c => c.parent === n.key)) // leaves: groups are containers, not work
    const live = kids.filter(n => n.status !== 'cancelled')
    const wants = g.nodes.filter(n => n.status === 'needs_you').length
    const [restart, setRestart] = useState(false)
    const [reason, setReason] = useState('')
    const [seen, setSeen] = useLocalStorage({key: 'epic-webhooks-dismissed', defaultValue: false})
    const {data: apps, reload: reloadApps} = useData(ListApps, [])
    const epic = g.stage !== ''
    const hasPR = g.prs.some(p => p.pr_url)
    const noHooks = !(apps ?? []).some(a => a.manifest.includes('"autostart"'))
    return <Stack gap="xs">
        <Paper withBorder radius="md" px="sm" py={6}>
            <Group justify="space-between" wrap="wrap">
                <Group gap="sm">
                    <Anchor component="button" fw={600} onClick={() => nav({name: 'task', key: root.key})}>{root.key}</Anchor>
                    <Text size="sm" truncate maw={320}>{root.title}</Text>
                    {epic && <Group gap={4}>{STAGES.map((s, i) => {
                        const at = STAGES.indexOf(g.stage)
                        return <Badge key={s} size="sm" variant={i === at ? 'filled' : 'light'} color={i === at ? 'blue' : i < at ? 'green' : 'gray'}>{i + 1}. {s}</Badge>
                    })}</Group>}
                </Group>
                <Group gap="sm">
                    <Text size="sm">{live.filter(n => n.status === 'done').length}/{live.length} done</Text>
                    <Badge color={wants ? 'orange' : 'gray'} variant={wants ? 'filled' : 'light'}>{wants} want{wants === 1 ? 's' : ''} you</Badge>
                    {g.gate && <Button size="compact-sm" color="green" onClick={() => EpicApprove(g.root).catch(errMsg)}>Approve plan</Button>}
                    {epic && <Button size="compact-sm" variant="default" onClick={() => setRestart(true)}>Restart</Button>}
                </Group>
            </Group>
        </Paper>
        {epic && <JiraSource epic={root.key}/>}
        {epic && hasPR && noHooks && !seen && (
            <Alert variant="light" withCloseButton onClose={() => setSeen(true)} title="Review comments arrive by backoff polling">
                <Group gap="sm">
                    <Text size="sm">Add the review-webhooks connector to get them as they are posted.</Text>
                    <Button size="compact-xs" onClick={() => InstallExample('review-webhooks').then(reloadApps, errMsg)}>Add review webhooks</Button>
                </Group>
            </Alert>
        )}
        <Modal opened={restart} onClose={() => setRestart(false)} title={`Restart ${g.root}?`}>
            <Stack gap="sm">
                <Text size="sm">Abandons the current flow, cancels its open sub-tasks and starts a new flow at spec.</Text>
                <Textarea data-autofocus label="Reason" required autosize minRows={2} value={reason} onChange={e => setReason(e.currentTarget.value)}/>
                <Group justify="flex-end">
                    <Button variant="default" onClick={() => setRestart(false)}>Cancel</Button>
                    <Button color="red" disabled={!reason.trim()} onClick={() => EpicRestart(g.root, reason).then(() => { setRestart(false); setReason('') }, errMsg)}>Restart</Button>
                </Group>
            </Stack>
        </Modal>
    </Stack>
}

const VIEW_HINT: Record<string, string> = {
    graph: 'Hover a card or a row below to trace what it waits on and what waits on it. Drag right handle → left handle to add a blocker.',
    waves: 'Tasks grouped by how many blockers deep they sit: a wave can run once the waves before it are done.',
    prs: "The flow's pull requests with review state, checks and unresolved comments.",
}

/** Open blockers of key: `blocks` links into it whose source isn't finished. */
const openBlockers = (g: core.Graph, key: string) => g.links
    .filter(l => l.kind === 'blocks' && l.to === key && !FINISHED.has(g.nodes.find(n => n.key === l.from)?.status ?? 'done'))
    .map(l => l.from)

/** One task as a list row: click opens it, hover traces it on the graph. */
function TaskRow({n, note}: {n: core.GraphNode; note?: string}) {
    const {open, hover} = useContext(Ctx)
    return (
        <Group gap={6} wrap="nowrap" px={6} py={2} style={{cursor: 'pointer', borderRadius: 4, borderLeft: `3px solid ${c6(KIND_COLOR[n.kind] ?? 'gray')}`}}
               className="epic-row" onClick={() => open(n.key)} onMouseEnter={() => hover(n.key)} onMouseLeave={() => hover('')}>
            <Text size="xs" fw={600} c="dimmed" style={{flexShrink: 0}}>{n.key}</Text>
            <StatusBadge status={n.status}/>
            <Text size="xs" truncate style={{flex: 1}}>{n.title}</Text>
            {note && <Text size="xs" c="dimmed" style={{flexShrink: 0}}>{note}</Text>}
        </Group>
    )
}

function Legend() {
    const swatch = (color: string, label: string, dashed = false) => (
        <Group key={label} gap={4} wrap="nowrap">
            <Box w={18} h={0} style={{borderTop: `2px ${dashed ? 'dashed' : 'solid'} ${c6(color)}`}}/>
            <Text size="xs" c="dimmed">{label}</Text>
        </Group>
    )
    return (
        <Group gap="md" wrap="wrap">
            {Object.entries(KIND_COLOR).map(([k, c]) => <Group key={k} gap={4} wrap="nowrap"><Box w={10} h={10} bg={c6(c)} style={{borderRadius: 2}}/><Text size="xs" c="dimmed">{k}</Text></Group>)}
            <Text size="xs" c="dimmed">·</Text>
            {swatch('orange', 'blocks (waiting)')}{swatch('cyan', 'blocker running')}{swatch('green', 'unblocked')}{swatch('gray', 'relates', true)}
        </Group>
    )
}

/** The boxes under the graph: what needs a human, what is moving, what can start, what is held back and by whom. */
function InfoBoxes({g}: {g: core.Graph}) {
    const live = leavesOf(g).filter(n => !FINISHED.has(n.status))
    const boxes: [string, string, {n: core.GraphNode; note?: string}[]][] = [
        ['Wants you', 'orange', live.filter(n => n.status === 'needs_you').map(n => ({n}))],
        ['Running', 'cyan', live.filter(n => n.status === 'running').map(n => ({n}))],
        ['Ready to run', 'blue', live.filter(n => READY.has(n.status) && openBlockers(g, n.key).length === 0).map(n => ({n}))],
        ['Waiting on', 'gray', live.flatMap(n => { const b = openBlockers(g, n.key); return b.length ? [{n, note: '← ' + b.join(', ')}] : [] })],
    ]
    return (
        <SimpleGrid cols={{base: 2, md: 4}} spacing="xs">
            {boxes.map(([title, color, rows]) => (
                <Paper key={title} withBorder radius="md" p="xs" style={{borderTop: `3px solid ${c6(color)}`}}>
                    <Group justify="space-between" mb={4}><Text size="xs" fw={700} tt="uppercase" c={color}>{title}</Text><Text size="xs" c="dimmed">{rows.length}</Text></Group>
                    <ScrollArea.Autosize mah={120}>
                        {rows.length ? rows.map(r => <TaskRow key={r.n.key} {...r}/>) : <Text size="xs" c="dimmed">None</Text>}
                    </ScrollArea.Autosize>
                </Paper>
            ))}
        </SimpleGrid>
    )
}

/** Leaves bucketed by blocker depth across the whole tree (groups ignored), so the order of work reads top to bottom. */
function Waves({g}: {g: core.Graph}) {
    const leaves = leavesOf(g).filter(n => n.status !== 'cancelled')
    const col = columns(leaves.map(n => n.key), g.links)
    const waves: core.GraphNode[][] = []
    for (const n of leaves) (waves[col.get(n.key)!] ??= []).push(n)
    return (
        <ScrollArea style={{flex: 1}}>
            <Stack gap="xs">
                {waves.map((ns, i) => {
                    const done = ns.filter(n => n.status === 'done').length
                    const color = done === ns.length ? 'green' : ns.some(n => n.status !== 'backlog' && n.status !== 'todo') ? 'cyan' : 'gray'
                    return (
                        <Paper key={i} withBorder radius="md" p="xs" style={{borderLeft: `4px solid ${c6(color)}`}}>
                            <Group justify="space-between" mb={4}>
                                <Text size="sm" fw={700}>Wave {i + 1}{i === 0 ? ' · no blockers' : ` · after wave ${i}`}</Text>
                                <Badge size="sm" variant="light" color={color}>{done}/{ns.length} done</Badge>
                            </Group>
                            {ns.map(n => <TaskRow key={n.key} n={n} note={openBlockers(g, n.key).length ? '← ' + openBlockers(g, n.key).join(', ') : undefined}/>)}
                        </Paper>
                    )
                })}
                {waves.length === 0 && <Text c="dimmed" size="sm">No tasks yet.</Text>}
            </Stack>
        </ScrollArea>
    )
}

function PRTable({g}: {g: core.Graph}) {
    const {open} = useContext(Ctx)
    const rows = g.prs.filter(p => p.pr_url)
    if (!rows.length) return <Text c="dimmed" size="sm">No PRs yet{g.stage ? '' : ' (PR rows are tracked for epics only)'}.</Text>
    const tone = (s?: string) => !s ? 'gray' : /approved|success|pass/i.test(s) ? 'green' : /changes|fail|error/i.test(s) ? 'red' : 'yellow'
    return (
        <ScrollArea style={{flex: 1}}>
            <Table highlightOnHover verticalSpacing={4}>
                <Table.Thead><Table.Tr><Table.Th>Task</Table.Th><Table.Th>Repo</Table.Th><Table.Th>PR</Table.Th><Table.Th>Review</Table.Th><Table.Th>Checks</Table.Th><Table.Th>Unresolved</Table.Th></Table.Tr></Table.Thead>
                <Table.Tbody>{rows.map(p => (
                    <Table.Tr key={p.task + p.repo}>
                        <Table.Td><Anchor component="button" size="sm" onClick={() => open(p.task)}>{p.task}</Anchor></Table.Td>
                        <Table.Td><Badge size="sm" variant="light">{p.repo}</Badge></Table.Td>
                        <Table.Td><Anchor size="sm" onClick={() => BrowserOpenURL(p.pr_url!)}>{p.pr_url!.replace(/^.*\/pull\//, '#')}</Anchor></Table.Td>
                        <Table.Td><Badge size="sm" variant="light" color={tone(p.review)}>{p.review || '—'}</Badge></Table.Td>
                        <Table.Td><Badge size="sm" variant="light" color={tone(p.checks)}>{p.checks || '—'}</Badge></Table.Td>
                        <Table.Td><Text size="sm" c={p.unresolved ? 'orange' : 'dimmed'}>{p.unresolved}</Text></Table.Td>
                    </Table.Tr>
                ))}</Table.Tbody>
            </Table>
        </ScrollArea>
    )
}

/** The epic view: the whole tree of key's root on a canvas, live on db:changed (useData reloads). */
/** `task` is the key in the URL (/epic/KEY/task/KEY): the open task modal, so it deep-links and Back closes it. */
export function EpicView({k, task = '', nav}: {k: string; task?: string; nav: Nav}) {
    const {data: g, error} = useData(() => EpicGraph(k), [k])
    const [adding, setAdding] = useState('')
    const setTask = (t: string) => nav({name: 'epic', key: k, task: t || undefined})
    const [title, setTitle] = useState('')
    const [nodes, setNodes, onNodesChange] = useNodesState<Node>([])
    const dragged = useRef(new Map<string, {x: number; y: number}>()) // a dragged card keeps its spot across live reloads

    // Leaf-card heights as React Flow measured them (cards size to content). A change re-runs the layout below; layout never changes a card's height, so this settles.
    const [heights, setHeights] = useState<ReadonlyMap<string, number>>(new Map())
    const measure: typeof onNodesChange = changes => {
        onNodesChange(changes)
        const next = new Map(heights)
        for (const c of changes) {
            // groups are sized by layout itself, so only leaves (no child points at them) are measured
            if (c.type === 'dimensions' && c.dimensions && !g?.nodes.some(n => n.parent === c.id)) next.set(c.id, Math.ceil(c.dimensions.height))
        }
        if ([...next].some(([k, h]) => heights.get(k) !== h)) setHeights(next)
    }
    // refactor: stale keys stay in `heights` after a task is removed; harmless (layout only looks up live keys).

    useEffect(() => {
        if (!g) return
        const byKey = new Map(g.nodes.map(n => [n.key, n]))
        const placed = layout(g.nodes.map(n => ({key: n.key, parent: n.parent})), g.links, heights)
        setNodes(placed.map(p => {
            const n = byKey.get(p.key)!
            const pos = dragged.current.get(p.key) ?? {x: p.x, y: p.y}
            return {id: p.key, type: p.group ? 'parent' : 'task', position: pos, parentId: p.parent, extent: p.parent ? 'parent' as const : undefined,
                    style: p.group ? {width: p.w, height: p.h} : undefined, zIndex: p.group ? 0 : 1,
                    data: p.group ? {n} : {n, prs: g.prs.filter(r => r.task === p.key)}}
        }))
    }, [g, heights])

    // Hover trace: the hovered task plus everything it waits on and everything waiting on it; the rest dims.
    const [hovered, setHovered] = useState('')
    const lit = useMemo(() => hovered && g ? trace(hovered, g.links) : null, [hovered, g])
    const [view, setView] = useLocalStorage({key: 'epic-view', defaultValue: 'graph'})

    const derived: Edge[] = useMemo(() => {
        const status = (k: string) => g?.nodes.find(n => n.key === k)?.status ?? ''
        return (g?.links ?? []).map(l => {
            const st = edgeState(l, status), on = !!lit && lit.has(l.from) && lit.has(l.to)
            const color = c6(st.color)
            return {
                id: `${l.kind}:${l.from}:${l.to}`, source: l.from, target: l.to, deletable: true, animated: st.animated,
                markerEnd: l.kind === 'blocks' ? {type: MarkerType.ArrowClosed, color} : undefined, // blocks: solid + arrow; relates: dashed
                style: {stroke: color, strokeWidth: on ? 2.5 : 1.5, opacity: lit && !on ? 0.15 : 1, strokeDasharray: l.kind === 'relates' ? '5 4' : undefined},
                // Labels only on the traced chain, as the reference diagram does; on every edge they bury the cards.
                label: on ? st.label : undefined, labelStyle: {fill: color, fontSize: 11}, labelBgStyle: {fill: 'var(--mantine-color-body)'},
            }
        })
    }, [g, lit])
    // Controlled edges need their own state or selection (and so delete-to-unlink) never sticks.
    const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([])
    useEffect(() => setEdges(derived), [derived, setEdges])

    if (error) return <Text c="red">{error}</Text>
    if (!g) return <Text c="dimmed">Loading…</Text>
    const root = g.nodes.find(n => n.key === g.root)!
    const ctx: Ctx = {nav, addSub: setAdding, open: setTask, lit, hover: setHovered}
    const connect = (c: Connection) => c.source && c.target && c.source !== c.target && Link(c.source, 'blocks', c.target).catch(errMsg)
    const create = () => CreateTask(core.NewTaskInput.createFrom({Title: title, ParentKey: adding})).then(() => { setAdding(''); setTitle('') }, errMsg)
    return (
        <Ctx.Provider value={ctx}>
            <Stack gap="sm" h="calc(100vh - 2 * var(--mantine-spacing-md))">
                <Header g={g} nav={nav} root={root}/>
                <Group justify="space-between" wrap="nowrap">
                    <SegmentedControl size="xs" value={view} onChange={setView} data={[
                        {value: 'graph', label: 'Graph'}, {value: 'waves', label: 'Waves'}, {value: 'prs', label: `PRs (${g.prs.filter(p => p.pr_url).length})`}]}/>
                    <Text size="xs" c="dimmed" truncate>{VIEW_HINT[view]}</Text>
                </Group>
                {view === 'graph' && <>
                    <Box style={{flex: 1, minHeight: 0, border: '1px solid var(--mantine-color-default-border)', borderRadius: 8}}>
                        <ReactFlow nodes={nodes} edges={edges} onEdgesChange={onEdgesChange} nodeTypes={nodeTypes} onNodesChange={measure} fitView minZoom={0.2}
                                   onNodeDragStop={(_, n) => dragged.current.set(n.id, n.position)}
                                   onConnect={connect}
                                   onEdgesDelete={es => es.forEach(e => { const [kind, from, to] = e.id.split(':'); Unlink(from, kind, to).catch(errMsg) })}>
                            <Background/>
                            <Controls showInteractive={false}/>
                        </ReactFlow>
                    </Box>
                    <Legend/>
                    <InfoBoxes g={g}/>
                </>}
                {view === 'waves' && <Waves g={g}/>}
                {view === 'prs' && <PRTable g={g}/>}
                <TaskPreview k={task ?? ''} onClose={() => setTask('')} nav={nav}/>
                <Modal opened={!!adding} onClose={() => setAdding('')} title={`New sub-task of ${adding}`}>
                    <form onSubmit={e => { e.preventDefault(); if (title.trim()) create() }}>
                        <Stack gap="sm">
                            <TextInput data-autofocus label="Title" value={title} onChange={e => setTitle(e.currentTarget.value)}/>
                            <Group justify="flex-end"><Button type="submit" disabled={!title.trim()}>Create</Button></Group>
                        </Stack>
                    </form>
                </Modal>
            </Stack>
        </Ctx.Provider>
    )
}
