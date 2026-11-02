import {createContext, useContext, useEffect, useMemo, useRef, useState} from 'react'
import {ActionIcon, Alert, Anchor, Badge, Box, Button, Group, Indicator, Menu, Modal, MultiSelect, Paper, Select, Stack, Text, Textarea, TextInput, Tooltip} from '@mantine/core'
import {useLocalStorage} from '@mantine/hooks'
import {IconCode, IconExternalLink, IconGitPullRequest, IconMessageQuestion, IconPlayerPlay, IconPlayerStop, IconPlus, IconTerminal2} from '@tabler/icons-react'
import {Background, Controls, Handle, MarkerType, Position, ReactFlow, useEdgesState, useNodesState, type Connection, type Edge, type Node, type NodeProps} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import {AddLabel, CreateTask, EpicApprove, EpicGraph, EpicRestart, ListAgents, ListApps, ListModels, ListSkills, Move, OpenApp, RemoveLabel, RunAction, SetTaskAgent, SetTaskModel, SetTaskSkills, Stop, Link, Unlink} from '../wailsjs/go/ui/App'
import {BrowserOpenURL} from '../wailsjs/runtime/runtime'
import {core, store} from '../wailsjs/go/models'
import {Nav, StatusBadge, errMsg} from './components'
import {layout} from './epicLayout'
import {useData} from './util'

// refactor: the card's pickers are 5 small bindings per card; a single `UpdateTask(key, patch)` binding would cut the round trips.

const STAGES = ['chart', 'decide', 'spec', 'build', 'verify', 'done']
const KINDS = ['grilling', 'prototype', 'research', 'build']
const KIND_COLOR: Record<string, string> = {grilling: 'orange', prototype: 'grape', research: 'teal', build: 'blue'}
const NO_RUN = new Set(['running', 'done'])

type Pickers = {agents: core.Agent[]; skills: core.Command[]; models: string[]; nav: Nav; addSub: (parent: string) => void}
const Ctx = createContext<Pickers>({agents: [], skills: [], models: [], nav: () => {}, addSub: () => {}})
type CardData = {n: core.GraphNode; prs: store.FlowPR[]}

const skillOf = (labels: string[]) => labels.filter(l => l.startsWith('skill:')).map(l => l.slice(6))
const hidden = (l: string) => /^(skill:|repo:|epic_kind:|epic_stage:|epic:|epic_gate:|epic_planner:)/.test(l) || l === 'epic'

function PrChip({p}: {p: store.FlowPR}) {
    const bits = [p.review, p.checks, p.unresolved ? `${p.unresolved} unresolved` : ''].filter(Boolean) // blank until the review check has data (sub-task A)
    return (
        <Tooltip label={p.pr_url}>
            <Badge component="button" className="nodrag" size="xs" variant="light" color="violet" leftSection={<IconGitPullRequest size={10}/>}
                   style={{cursor: 'pointer', textTransform: 'none'}} onClick={() => p.pr_url && BrowserOpenURL(p.pr_url)}>
                {p.repo}{bits.length > 0 && ' · ' + bits.join(' · ')}
            </Badge>
        </Tooltip>
    )
}

function TaskNode({data}: NodeProps<Node<CardData>>) {
    const {n, prs} = data
    const {agents, skills, models, nav} = useContext(Ctx)
    const agent = agents.find(a => a.name === n.agent)
    const running = n.status === 'running'
    const [editors, setEditors] = useState<store.App[]>([])
    const stuck = n.status === 'needs_you'
    // Every control gets `nodrag nowheel` so React Flow doesn't start a node drag or zoom the canvas (the known pitfall with interactive nodes).
    const nd = 'nodrag nowheel'
    return (
        <Paper withBorder radius="md" p="xs" w={320} shadow="sm">
            <Handle type="target" position={Position.Left}/>
            <Handle type="source" position={Position.Right}/>
            <Stack gap={6}>
                <Group justify="space-between" wrap="nowrap" gap="xs">
                    <Group gap={6} wrap="nowrap">
                        <Text size="xs" fw={600} c="dimmed">{n.key}</Text>
                        <Indicator disabled={!running} processing color="cyan" size={8} offset={-2}><StatusBadge status={n.status}/></Indicator>
                    </Group>
                    <Group gap={2} wrap="nowrap" className={nd}>
                        <Tooltip label="Open task"><ActionIcon size="sm" variant="subtle" aria-label="Open task" onClick={() => nav({name: 'task', key: n.key})}><IconExternalLink size={14}/></ActionIcon></Tooltip>
                        {running
                            ? <Tooltip label="Stop the run"><ActionIcon size="sm" variant="subtle" color="red" aria-label="Stop" onClick={() => Stop(n.key).catch(errMsg)}><IconPlayerStop size={14}/></ActionIcon></Tooltip>
                            : !NO_RUN.has(n.status) && <Tooltip label={n.status === 'todo' ? 'Queued' : n.status === 'backlog' ? 'Run' : 'Requeue'}><ActionIcon size="sm" variant="subtle" aria-label="Run" disabled={n.status === 'todo'} onClick={() => Move(n.key, 'todo', false).catch(errMsg)}><IconPlayerPlay size={14}/></ActionIcon></Tooltip>}
                        {stuck && <Tooltip label="Answer on the task page"><ActionIcon size="sm" variant="subtle" color="orange" aria-label="Answer" onClick={() => nav({name: 'task', key: n.key})}><IconMessageQuestion size={14}/></ActionIcon></Tooltip>}
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
                <Group gap={4}>
                    <Menu withinPortal>
                        <Menu.Target><Badge className={nd} size="xs" component="button" style={{cursor: 'pointer'}} color={KIND_COLOR[n.kind] ?? 'gray'} variant="filled">{n.kind || 'no kind'}</Badge></Menu.Target>
                        <Menu.Dropdown>{KINDS.map(k => <Menu.Item key={k} onClick={() => setKind(n, k)}>{k}</Menu.Item>)}</Menu.Dropdown>
                    </Menu>
                    {n.repos.map(r => <Badge key={r} size="xs" variant="light">{r}</Badge>)}
                    {n.worktrees.length > 0 && <Tooltip label={'worktree: ' + n.worktrees.join(', ')}><Badge size="xs" variant="outline" color="gray">worktree</Badge></Tooltip>}
                    {prs.filter(p => p.pr_url).map(p => <PrChip key={p.repo} p={p}/>)}
                </Group>
                <Select className={nd} size="xs" label="Agent" clearable searchable placeholder="none" value={n.agent || null}
                        data={agents.map(a => ({value: a.name, label: `${a.name} (${a.scope})`}))}
                        onChange={v => SetTaskAgent(n.key, v ?? '').catch(errMsg)}/>
                <Select className={nd} size="xs" label="Model" clearable searchable placeholder={agent?.model || 'default'} value={n.model || null}
                        data={models} onChange={v => SetTaskModel(n.key, v ?? '').catch(errMsg)}/>
                <MultiSelect className={nd} size="xs" label="Skills" searchable placeholder="add a skill" value={skillOf(n.labels)}
                             data={skills.map(s => s.name)} onChange={v => SetTaskSkills(n.key, v).catch(errMsg)}/>
                {agent && agent.skills.length > 0 && <Group gap={4}><Text size="xs" c="dimmed">from agent</Text>{agent.skills.map(s => <Badge key={s} size="xs" variant="outline" color="gray">{s}</Badge>)}</Group>}
                {n.labels.filter(l => !hidden(l)).length > 0 && <Group gap={4}>{n.labels.filter(l => !hidden(l)).map(l => <Badge key={l} size="xs" variant="outline" color="gray" style={{textTransform: 'none'}}>{l}</Badge>)}</Group>}
            </Stack>
        </Paper>
    )
}

/** epic_kind is a label: swap the old one for the new one. refactor: a SetLabel(key, "epic_kind:x") binding over core.SetLabel would be one call and atomic. */
const setKind = (n: core.GraphNode, kind: string) =>
    (n.kind ? RemoveLabel(n.key, 'epic_kind:' + n.kind) : Promise.resolve()).then(() => AddLabel(n.key, 'epic_kind:' + kind)).catch(errMsg)

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
    const {data: apps} = useData(ListApps, [])
    const epic = g.stage !== ''
    const hasPR = g.prs.some(p => p.pr_url)
    const noHooks = !(apps ?? []).some(a => /review.?webhooks/i.test(a.name))
    return <Stack gap="xs">
        <Paper withBorder radius="md" px="sm" py={6}>
            <Group justify="space-between" wrap="wrap">
                <Group gap="sm">
                    <Anchor component="button" fw={600} onClick={() => nav({name: 'task', key: root.key})}>{root.key}</Anchor>
                    <Text size="sm" truncate maw={320}>{root.title}</Text>
                    {epic && <Group gap={4}>{STAGES.map((s, i) => <Badge key={s} size="sm" variant={s === g.stage ? 'filled' : 'light'} color={s === g.stage ? 'blue' : 'gray'}>{i + 1}. {s}</Badge>)}</Group>}
                </Group>
                <Group gap="sm">
                    <Text size="sm">{live.filter(n => n.status === 'done').length}/{live.length} done</Text>
                    <Badge color={wants ? 'orange' : 'gray'} variant={wants ? 'filled' : 'light'}>{wants} want{wants === 1 ? 's' : ''} you</Badge>
                    {g.gate && <Button size="compact-sm" color="green" onClick={() => EpicApprove(g.root).catch(errMsg)}>Approve plan</Button>}
                    {epic && <Button size="compact-sm" variant="default" onClick={() => setRestart(true)}>Restart</Button>}
                </Group>
            </Group>
        </Paper>
        {epic && hasPR && noHooks && !seen && (
            <Alert variant="light" withCloseButton onClose={() => setSeen(true)} title="Review comments arrive by backoff polling">
                <Group gap="sm">
                    <Text size="sm">Add the review-webhooks app to get them as they are posted.</Text>
                    <Button size="compact-xs" onClick={() => nav({name: 'apps'})}>Add review webhooks</Button>
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

/** The epic view: the whole tree of key's root on a canvas, live on db:changed (useData reloads). */
export function EpicView({k, nav}: {k: string; nav: Nav}) {
    const {data: g, error} = useData(() => EpicGraph(k), [k])
    const repos = useMemo(() => [...new Set((g?.nodes ?? []).flatMap(n => n.repos))].sort(), [g])
    const {data: agents} = useData(() => ListAgents(repos), [repos.join()])
    const {data: skills} = useData(() => ListSkills(repos), [repos.join()])
    const {data: models} = useData(ListModels, [])
    const [adding, setAdding] = useState('')
    const [title, setTitle] = useState('')
    const [nodes, setNodes, onNodesChange] = useNodesState<Node>([])
    const dragged = useRef(new Map<string, {x: number; y: number}>()) // a dragged card keeps its spot across live reloads

    useEffect(() => {
        if (!g) return
        const byKey = new Map(g.nodes.map(n => [n.key, n]))
        const placed = layout(g.nodes.map(n => ({key: n.key, parent: n.parent})), g.links)
        setNodes(placed.map(p => {
            const n = byKey.get(p.key)!
            const pos = dragged.current.get(p.key) ?? {x: p.x, y: p.y}
            return {id: p.key, type: p.group ? 'parent' : 'task', position: pos, parentId: p.parent, extent: p.parent ? 'parent' as const : undefined,
                    style: p.group ? {width: p.w, height: p.h} : undefined, zIndex: p.group ? 0 : 1,
                    data: p.group ? {n} : {n, prs: g.prs.filter(r => r.task === p.key)}}
        }))
    }, [g])

    const derived: Edge[] = useMemo(() => (g?.links ?? []).map(l => ({
        id: `${l.kind}:${l.from}:${l.to}`, source: l.from, target: l.to, deletable: true,
        markerEnd: l.kind === 'blocks' ? {type: MarkerType.ArrowClosed} : undefined, // blocks: solid + arrow; relates: dashed
        style: l.kind === 'relates' ? {strokeDasharray: '5 4'} : undefined,
    })), [g])
    // Controlled edges need their own state or selection (and so delete-to-unlink) never sticks.
    const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([])
    useEffect(() => setEdges(derived), [derived, setEdges])

    if (error) return <Text c="red">{error}</Text>
    if (!g) return <Text c="dimmed">Loading…</Text>
    const root = g.nodes.find(n => n.key === g.root)!
    const ctx: Pickers = {agents: agents ?? [], skills: skills ?? [], models: (models?.models ?? []).map(m => m.id), nav, addSub: setAdding}
    const connect = (c: Connection) => c.source && c.target && c.source !== c.target && Link(c.source, 'blocks', c.target).catch(errMsg)
    const create = () => CreateTask(core.NewTaskInput.createFrom({Title: title, ParentKey: adding})).then(() => { setAdding(''); setTitle('') }, errMsg)
    return (
        <Ctx.Provider value={ctx}>
            <Stack gap="sm" h="calc(100vh - 2 * var(--mantine-spacing-md))">
                <Header g={g} nav={nav} root={root}/>
                <Box style={{flex: 1, minHeight: 0, border: '1px solid var(--mantine-color-default-border)', borderRadius: 8}}>
                    <ReactFlow nodes={nodes} edges={edges} onEdgesChange={onEdgesChange} nodeTypes={nodeTypes} onNodesChange={onNodesChange} fitView minZoom={0.2}
                               onNodeDragStop={(_, n) => dragged.current.set(n.id, n.position)}
                               onConnect={connect}
                               onEdgesDelete={es => es.forEach(e => { const [kind, from, to] = e.id.split(':'); Unlink(from, kind, to).catch(errMsg) })}>
                        <Background/>
                        <Controls showInteractive={false}/>
                    </ReactFlow>
                </Box>
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
