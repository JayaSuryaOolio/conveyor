import {useState} from 'react'
import {ActionIcon, Badge, TextInput, Tooltip} from '@mantine/core'
import {Group, Stack, Text} from './ui'
import {IconAlertTriangle, IconCheck, IconPlus, IconTrash} from '@tabler/icons-react'
import {AddDecision, Decisions as ListDecisions, DisputeDecision, RemoveDecision} from '../wailsjs/go/ui/App'
import {errMsg} from './components'
import {useData} from './util'

/** Decisions and findings (facts that came to light in reviews, CI, tests: agents add them as `finding:` lines) recorded on a task ("what — why"): the human adds, strikes, disputes or unmarks one. Agents add theirs in the status block. */
export function Decisions({k}: {k: string}) {
    const {data, reload} = useData(() => ListDecisions(k), [k])
    const [text, setText] = useState('')
    const done = (p: Promise<void>) => p.then(reload).catch(errMsg)
    const dispute = (id: number) => { const r = window.prompt('Why is this decision a mistake? (one line)'); if (r) done(DisputeDecision(k, id, r)) }
    const add = () => { if (text.trim()) done(AddDecision(k, text)).then(() => setText('')) }
    return (
        <Stack gap={4}>
            {(data ?? []).map(d => (
                <Stack key={d.id} gap={0}>
                    <Group gap={4} wrap={false} align="start">
                        <Text size="sm" style={{flex: 1}}><b>D{d.id}</b> ({d.author}) {d.kind === 'finding' && <Badge size="xs" variant="light" color="teal" mr={4}>finding</Badge>}{d.body}</Text>
                        <Tooltip label={d.disputed_by ? 'Resolved: remove the pitfall' : 'Dispute'}>
                            <ActionIcon size="sm" variant="subtle" color={d.disputed_by ? 'green' : 'orange'} aria-label={d.disputed_by ? 'Unmark dispute' : 'Dispute decision'}
                                        onClick={() => d.disputed_by ? done(DisputeDecision(k, d.id, '')) : dispute(d.id)}>
                                {d.disputed_by ? <IconCheck size={14}/> : <IconAlertTriangle size={14}/>}
                            </ActionIcon>
                        </Tooltip>
                        <ActionIcon size="sm" variant="subtle" color="gray" aria-label="Strike decision" onClick={() => done(RemoveDecision(k, d.id))}><IconTrash size={14}/></ActionIcon>
                    </Group>
                    {d.disputed_by && <Text size="xs" tone="warning" style={{marginLeft: 16}}>pitfall ({d.disputed_by}): {d.dispute}</Text>}
                </Stack>
            ))}
            <Group gap={4} wrap={false}>
                <TextInput size="xs" style={{flex: 1}} placeholder="decision: what — why" value={text} onChange={e => setText(e.currentTarget.value)} onKeyDown={e => e.key === 'Enter' && add()}/>
                <ActionIcon variant="default" size="md" aria-label="Add decision" disabled={!text.trim()} onClick={add}><IconPlus size={14}/></ActionIcon>
            </Group>
        </Stack>
    )
}
