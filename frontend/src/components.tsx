import {useState} from 'react'
import {ActionIcon, Badge, Button, Card as MCard, Group, Menu, Paper, Select, Stack, Text, Textarea, Tooltip} from '@mantine/core'
import {IconDots} from '@tabler/icons-react'
import {AddComment, Move, ShowError} from '../wailsjs/go/ui/App'
import {store, ui} from '../wailsjs/go/models'
import {ALL_STATUSES, STATUS_COLOR, STATUS_LABEL, ago, fmtK, fmtTime} from './util'

export type Nav = (v: {name: string; key?: string}) => void

export const errMsg = (e: unknown) => ShowError(String(e))

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
                <Text size="xs" fw={600} c="dimmed">{c.key}</Text>
                <Group gap={4} wrap="nowrap">
                    {c.ctxPeak > 0 && <Badge variant="outline" color="gray" size="xs" title="context peak">{fmtK(c.ctxPeak)}</Badge>}
                    <StatusMenu k={c.key} status={c.status}/>
                </Group>
            </Group>
            <Text size="sm" fw={500} mt={2}>{c.title}</Text>
            {c.repos.length > 0 && <Group gap={4} mt="xs">{c.repos.map(r => <Badge key={r} variant="light" size="xs">{r}</Badge>)}</Group>}
            {c.lastSummary && <Text size="xs" c="dimmed" mt="xs" lineClamp={2}>{c.lastSummary}</Text>}
            <Group mt="xs"><StatusBadge status={c.status}/></Group>
        </MCard>
    )
}

export function CommentBox({k, placeholder = 'Comment…', onDone}: {k: string; placeholder?: string; onDone?: () => void}) {
    const [text, setText] = useState('')
    const send = () => {
        if (!text.trim()) return
        AddComment(k, text).then(() => { setText(''); onDone?.() }, errMsg)
    }
    return (
        <Stack gap="xs" align="flex-end">
            <Textarea autosize minRows={2} maxRows={10} w="100%" value={text} placeholder={placeholder} onChange={e => setText(e.target.value)}
                      onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) send() }}/>
            <Button size="xs" onClick={send} disabled={!text.trim()}>Send</Button>
        </Stack>
    )
}

/** One comment in the thread. */
export function Message({m}: {m: store.Comment}) {
    return (
        <Paper withBorder p="sm" radius="md" bg={m.author === 'human' ? 'var(--mantine-color-default-hover)' : undefined}>
            <Group gap="xs" mb={4}>
                <Text size="xs" fw={600}>{m.author}</Text>
                <Time iso={m.created_at}/>
            </Group>
            <Text size="sm" style={{whiteSpace: 'pre-wrap', wordBreak: 'break-word'}}>{m.body}</Text>
        </Paper>
    )
}
