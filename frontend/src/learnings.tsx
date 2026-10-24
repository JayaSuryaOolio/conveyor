import {useState} from 'react'
import {Button, Group, Stack, Text, TextInput, Title} from '@mantine/core'
import {AcceptLearning, ApplyLearnings, EditLearning, Learnings as ListLearnings, RejectLearning} from '../wailsjs/go/ui/App'
import {ui} from '../wailsjs/go/models'
import {Nav, errMsg} from './components'
import {useData} from './util'

export function LearningsPage({nav}: {nav: Nav}) {
    const {data, error, reload} = useData(ListLearnings, [])
    const groups: Record<string, ui.LearningRow[]> = {}
    data?.forEach(l => (groups[l.repo || '(personal)'] ||= []).push(l))
    return (
        <Stack maw={1000}>
            <Title order={3}>Learnings</Title>
            {error && <Text c="red">{error}</Text>}
            {data?.length === 0 && <Text c="dimmed">Nothing proposed.</Text>}
            {Object.keys(groups).sort().map(repo => (
                <Stack key={repo} gap="xs">
                    <Group justify="space-between">
                        <Title order={5}>{repo}</Title>
                        {repo !== '(personal)' && groups[repo].some(l => l.status === 'accepted') && (
                            <Button size="xs" onClick={() => ApplyLearnings(repo).then(k => k && nav({name: 'task', key: k}), errMsg)}>Apply to {repo}</Button>
                        )}
                    </Group>
                    {groups[repo].map(l => <LearningItem key={l.id} l={l} reload={reload}/>)}
                </Stack>
            ))}
        </Stack>
    )
}

function LearningItem({l, reload}: {l: ui.LearningRow; reload: () => void}) {
    const [body, setBody] = useState(l.body)
    const [target, setTarget] = useState(l.target ?? 'CLAUDE.md')
    const done = () => reload()
    return (
        <Group wrap="nowrap" gap="xs">
            <TextInput style={{flex: 1}} value={body} onChange={e => setBody(e.target.value)}/>
            <Button variant="default" disabled={body === l.body} onClick={() => EditLearning(l.id, body).then(done, errMsg)}>Save</Button>
            <TextInput w={140} title="target file" value={target} onChange={e => setTarget(e.target.value)}/>
            <Button color={l.status === 'accepted' ? 'green' : undefined} disabled={l.status === 'accepted' && target === l.target} onClick={() => AcceptLearning(l.id, target).then(done, errMsg)}>
                {l.status === 'accepted' ? 'Accepted' : 'Accept'}
            </Button>
            <Button variant="default" color="red" onClick={() => RejectLearning(l.id).then(done, errMsg)}>Reject</Button>
        </Group>
    )
}
