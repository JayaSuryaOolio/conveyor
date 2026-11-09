import {useState} from 'react'
import {ActionIcon, Anchor, Badge, Group, Select, Stack, Text, Title, Tooltip} from '@mantine/core'
import {IconChevronDown, IconChevronRight, IconPlus, IconTrash} from '@tabler/icons-react'
import {AdoptChild, Link, SetParent, Unlink} from '../wailsjs/go/ui/App'
import {store, ui} from '../wailsjs/go/models'
import {Nav, StatusBadge, errMsg} from './components'
import {listTasks, useData} from './util'

// Choices as seen from this task: [value, label, kind, this task is the link's from side (blocker / parent)].
const CHOICES: [string, string, string, boolean][] = [
    ['blocks', 'blocks', 'blocks', true], ['blocked', 'is blocked by', 'blocks', false],
    ['relates', 'relates to', 'relates', true],
    ['parent', 'is parent of', 'parent', true], ['subtask', 'is sub-task of', 'parent', false],
]

/** Tasks that never appear in the picker. */
const closed = (t: ui.TaskCard) => t.status === 'done' || t.status === 'cancelled'

/** Keys of tasks that can't take part in the chosen relation: already related that way, it would close a loop, or (is parent of) it already has a parent. */
export function disabledKeys(choice: string, rel: store.Relations, tasks: ui.TaskCard[] = []): string[] {
    const [, , kind, from] = CHOICES.find(c => c[0] === choice)!
    if (kind === 'parent') {
        return from ? [...rel.ancestors, ...rel.children.map(c => c.key), ...tasks.filter(t => t.parent).map(t => t.key)] : [...rel.descendants, ...(rel.parent ? [rel.parent.key] : [])]
    }
    const linked = rel.links.filter(l => l.kind === kind && (kind === 'relates' || l.from === from)).map(l => l.task.key)
    return kind === 'blocks' ? [...linked, ...(from ? rel.blocked_by_all : rel.blocks_all)] : linked
}

type Kids = Map<string, ui.TaskCard[]>

/** Task row; when the task has sub-tasks it gets a chevron that expands them nested 1.25rem (20px, scales with font) right, recursively. Collapsed shows "(2/3 subtasks DONE)". */
// Refactor: Row/status counting could move to the backend (store.Relations children of linked tasks) if ListTasks gets heavy.
function Row({task, nav, kids, onRemove}: {task: store.TaskRef; nav: Nav; kids: Kids; onRemove?: () => void}) {
    const [open, setOpen] = useState(false)
    const subs = kids.get(task.key) ?? []
    const done = subs.filter(s => s.status === 'done').length
    return (
        <Stack gap={2}>
            <Group gap={6} wrap="nowrap" justify="space-between">
                <Group gap={6} wrap="nowrap" miw={0}>
                    {subs.length > 0
                        ? <ActionIcon variant="subtle" color="gray" size="xs" aria-label={open ? 'Collapse sub-tasks' : 'Expand sub-tasks'} aria-expanded={open} onClick={() => setOpen(!open)}>
                            {open ? <IconChevronDown size={14}/> : <IconChevronRight size={14}/>}
                        </ActionIcon>
                        : <span style={{width: 18, flex: 'none'}}/>}
                    <Anchor size="sm" component="button" ta="left" onClick={() => nav({name: 'task', key: task.key})}>{task.key}</Anchor>
                    <Text size="sm" lineClamp={1} title={task.title}>{task.title}</Text>
                    {subs.length > 0 && !open && <Text size="xs" c="dimmed" style={{whiteSpace: 'nowrap'}}>({done}/{subs.length} subtasks DONE)</Text>}
                </Group>
                <Group gap={4} wrap="nowrap">
                    <StatusBadge status={task.status}/>
                    {onRemove && (
                        <Tooltip label="Remove link">
                            <ActionIcon variant="subtle" color="gray" size="sm" aria-label="Remove link" onClick={onRemove}><IconTrash size={14}/></ActionIcon>
                        </Tooltip>
                    )}
                </Group>
            </Group>
            {open && <Stack gap={2} ml="1.25rem">{subs.map(c => <Row key={c.key} task={c} nav={nav} kids={kids}/>)}</Stack>}
        </Stack>
    )
}

/** Parent, sub-tasks and links of a task, with add / remove. */
export function Relations({k, status, rel, nav, reload}: {k: string; status: string; rel: store.Relations; nav: Nav; reload: () => void}) {
    const {data: tasks} = useData(listTasks, [])
    const [choice, setChoice] = useState('blocks')
    const [other, setOther] = useState<string | null>(null)
    const open = (s: string) => s !== 'done' && s !== 'cancelled'
    const blockers = rel.links.filter(l => l.kind === 'blocks' && !l.from && open(l.task.status))
    const done = (p: Promise<void>) => p.then(reload).catch(errMsg)
    const add = () => {
        const [, , kind, from] = CHOICES.find(c => c[0] === choice)!
        if (!other) return
        const [a, b] = from ? [k, other] : [other, k]
        done(kind === 'parent' ? (from ? AdoptChild(a, b) : SetParent(b, a)) : Link(a, kind, b)).then(() => setOther(null))
    }
    const options = (tasks ?? []).filter(x => x.key !== k && !closed(x))
    const off = disabledKeys(choice, rel, options)
    const hasParent = new Set(choice === 'parent' ? options.filter(t => t.parent && !rel.children.some(c => c.key === t.key)).map(t => t.key) : [])
    const kids: Kids = new Map()
    for (const t of tasks ?? []) if (t.parent) kids.set(t.parent, [...(kids.get(t.parent) ?? []), t])
    const byKey = new Map(options.map(t => [t.key, t]))
    const labels = [...new Set(rel.links.map(l => l.label))]
    return (
        <Stack gap="xs">
            <Title order={5}>Relations</Title>
            {status === 'todo' && blockers.length > 0 && <Text size="xs" c="orange">Not launched: blocked by {blockers.map(l => l.task.key).join(', ')}</Text>}
            {rel.parent && <Stack gap={2}><Text size="xs" c="dimmed">Parent</Text><Row task={rel.parent} nav={nav} kids={kids} onRemove={() => done(SetParent(k, ''))}/></Stack>}
            {rel.children.length > 0 && (
                <Stack gap={2}>
                    <Text size="xs" c="dimmed">Sub-tasks</Text>
                    {rel.children.map(c => <Row key={c.key} task={c} nav={nav} kids={kids} onRemove={() => done(SetParent(c.key, ''))}/>)}
                </Stack>
            )}
            {labels.map(label => (
                <Stack key={label} gap={2}>
                    <Text size="xs" c="dimmed">{label}</Text>
                    {rel.links.filter(l => l.label === label).map(l => (
                        <Row key={l.kind + l.task.key} task={l.task} nav={nav} kids={kids}
                             onRemove={() => done(l.from ? Unlink(k, l.kind, l.task.key) : Unlink(l.task.key, l.kind, k))}/>
                    ))}
                </Stack>
            ))}
            <Group gap={4} wrap="nowrap">
                <Select size="xs" w={130} allowDeselect={false} value={choice} onChange={v => { if (v) { setChoice(v); setOther(null) } }} data={CHOICES.map(c => ({value: c[0], label: c[1]}))}/>
                <Select size="xs" clearable placeholder="Task" style={{flex: 1}} value={other} onChange={setOther}
                        data={options.map(x => ({value: x.key, label: `${x.key} ${x.title}`, disabled: off.includes(x.key)}))}
                        renderOption={({option}) => (
                            <Group gap={6} wrap="nowrap" justify="space-between" w="100%">
                                <Text size="xs" lineClamp={1}>{option.label}</Text>
                                <Group gap={4} wrap="nowrap">
                                    {hasParent.has(option.value) && <Badge variant="outline" color="gray" size="xs">has parent</Badge>}
                                    <StatusBadge status={byKey.get(option.value)?.status ?? ''}/>
                                </Group>
                            </Group>
                        )}/>
                <ActionIcon variant="default" size="md" aria-label="Add link" disabled={!other} onClick={add}><IconPlus size={14}/></ActionIcon>
            </Group>
        </Stack>
    )
}
