import {useState} from 'react'
import {ActionIcon, Anchor, Group, Select, Stack, Text, Title, Tooltip} from '@mantine/core'
import {IconPlus, IconTrash} from '@tabler/icons-react'
import {Link, ListTasks, SetParent, Unlink} from '../wailsjs/go/ui/App'
import {store} from '../wailsjs/go/models'
import {Nav, StatusBadge, errMsg} from './components'
import {useData} from './util'

// Choices as seen from this task: [value, label, kind, this task is the link's from side (blocker / parent)].
const CHOICES: [string, string, string, boolean][] = [
    ['blocks', 'blocks', 'blocks', true], ['blocked', 'is blocked by', 'blocks', false],
    ['relates', 'relates to', 'relates', true],
    ['parent', 'is parent of', 'parent', true], ['subtask', 'is sub-task of', 'parent', false],
]

/** Keys of tasks that can't take part in the chosen relation: already related that way, or it would close a loop. */
export function disabledKeys(choice: string, rel: store.Relations): string[] {
    const [, , kind, from] = CHOICES.find(c => c[0] === choice)!
    if (kind === 'parent') {
        return from ? [...rel.ancestors, ...rel.children.map(c => c.key)] : [...rel.descendants, ...(rel.parent ? [rel.parent.key] : [])]
    }
    const linked = rel.links.filter(l => l.kind === kind && (kind === 'relates' || l.from === from)).map(l => l.task.key)
    return kind === 'blocks' ? [...linked, ...(from ? rel.blocked_by_all : rel.blocks_all)] : linked
}

function Row({task, nav, onRemove}: {task: store.TaskRef; nav: Nav; onRemove: () => void}) {
    return (
        <Group gap={6} wrap="nowrap" justify="space-between">
            <Group gap={6} wrap="nowrap" miw={0}>
                <Anchor size="sm" component="button" ta="left" onClick={() => nav({name: 'task', key: task.key})}>{task.key}</Anchor>
                <Text size="sm" lineClamp={1} title={task.title}>{task.title}</Text>
            </Group>
            <Group gap={4} wrap="nowrap">
                <StatusBadge status={task.status}/>
                <Tooltip label="Remove link">
                    <ActionIcon variant="subtle" color="gray" size="sm" aria-label="Remove link" onClick={onRemove}><IconTrash size={14}/></ActionIcon>
                </Tooltip>
            </Group>
        </Group>
    )
}

/** Parent, sub-tasks and links of a task, with add / remove. */
export function Relations({k, status, rel, nav, reload}: {k: string; status: string; rel: store.Relations; nav: Nav; reload: () => void}) {
    const {data: tasks} = useData(ListTasks, [])
    const [choice, setChoice] = useState('blocks')
    const [other, setOther] = useState<string | null>(null)
    const open = (s: string) => s !== 'done' && s !== 'cancelled'
    const blockers = rel.links.filter(l => l.kind === 'blocks' && !l.from && open(l.task.status))
    const done = (p: Promise<void>) => p.then(reload).catch(errMsg)
    const add = () => {
        const [, , kind, from] = CHOICES.find(c => c[0] === choice)!
        if (!other) return
        const [a, b] = from ? [k, other] : [other, k]
        done(kind === 'parent' ? SetParent(b, a) : Link(a, kind, b)).then(() => setOther(null))
    }
    const off = disabledKeys(choice, rel)
    const labels = [...new Set(rel.links.map(l => l.label))]
    return (
        <Stack gap="xs">
            <Title order={5}>Relations</Title>
            {status === 'todo' && blockers.length > 0 && <Text size="xs" c="orange">Not launched: blocked by {blockers.map(l => l.task.key).join(', ')}</Text>}
            {rel.parent && <Stack gap={2}><Text size="xs" c="dimmed">Parent</Text><Row task={rel.parent} nav={nav} onRemove={() => done(SetParent(k, ''))}/></Stack>}
            {rel.children.length > 0 && (
                <Stack gap={2}>
                    <Text size="xs" c="dimmed">Sub-tasks</Text>
                    {rel.children.map(c => <Row key={c.key} task={c} nav={nav} onRemove={() => done(SetParent(c.key, ''))}/>)}
                </Stack>
            )}
            {labels.map(label => (
                <Stack key={label} gap={2}>
                    <Text size="xs" c="dimmed">{label}</Text>
                    {rel.links.filter(l => l.label === label).map(l => (
                        <Row key={l.kind + l.task.key} task={l.task} nav={nav}
                             onRemove={() => done(l.from ? Unlink(k, l.kind, l.task.key) : Unlink(l.task.key, l.kind, k))}/>
                    ))}
                </Stack>
            ))}
            <Group gap={4} wrap="nowrap">
                <Select size="xs" w={130} allowDeselect={false} value={choice} onChange={v => { if (v) { setChoice(v); setOther(null) } }} data={CHOICES.map(c => ({value: c[0], label: c[1]}))}/>
                <Select size="xs" searchable clearable placeholder="Task" style={{flex: 1}} value={other} onChange={setOther}
                        data={(tasks ?? []).filter(x => x.key !== k).map(x => ({value: x.key, label: `${x.key} ${x.title}`, disabled: off.includes(x.key)}))}/>
                <ActionIcon variant="default" size="md" aria-label="Add link" disabled={!other} onClick={add}><IconPlus size={14}/></ActionIcon>
            </Group>
        </Stack>
    )
}
