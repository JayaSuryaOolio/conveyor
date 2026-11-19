import {useState} from 'react'
import {Text, TextInput, Title} from '@mantine/core'
import {Button, Group, Stack} from './ui'
import {AcceptLearning, ApplyLearnings, EditLearning, Learnings as ListLearnings, RejectLearning, RejectLearningCategory, RejectSimilarLearnings} from '../wailsjs/go/ui/App'
import {ui} from '../wailsjs/go/models'
import {Nav, errMsg} from './components'
import {useData} from './util'

export function LearningsPage({nav}: {nav: Nav}) {
    const {data, error, reload} = useData(ListLearnings, [])
    const groups: Record<string, ui.LearningRow[]> = {}
    data?.forEach(l => (groups[l.repo || '(personal)'] ||= []).push(l))
    return (
        <Stack style={{maxWidth: 1000}}>
            <Title order={3}>Learnings</Title>
            {error && <Text c="red">{error}</Text>}
            <Group gap="xs">
                {['placeholder', 'personal-env'].map(c => (
                    <Button key={c} size="xs" variant="secondary" tone="danger" onClick={() => RejectLearningCategory(c).then(reload, errMsg)}>Reject all {c}</Button>
                ))}
            </Group>
            {data?.length === 0 && <Text c="dimmed">Nothing proposed.</Text>}
            {Object.keys(groups).sort().map(repo => (
                <Stack key={repo} gap="xs">
                    <Group justify="between">
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
    const [target, setTarget] = useState(l.target ?? (l.repo ? 'local' : 'CLAUDE.md'))
    const done = () => reload()
    return (
        <Group wrap={false} gap="xs">
            <Text size="xs" c="dimmed" miw={32} title="Learning ID, as in `conveyor learning reject ID`">#{l.id}</Text>
            <TextInput style={{flex: 1}} value={body} onChange={e => setBody(e.target.value)}/>
            <Button variant="secondary" disabled={body === l.body} onClick={() => EditLearning(l.id, body).then(done, errMsg)}>Save</Button>
            <TextInput w={140} title="local = Conveyor's LEARNINGS.md for this repo, loaded into its tasks; a repo file (CLAUDE.md) = PR via Apply" value={target} onChange={e => setTarget(e.target.value)}/>
            <Button tone={l.status === 'accepted' ? 'success' : undefined} disabled={l.status === 'accepted' && target === l.target} onClick={() => AcceptLearning(l.id, target).then(done, errMsg)}>
                {l.status === 'accepted' ? 'Accepted' : 'Accept'}
            </Button>
            <Button variant="secondary" tone="danger" onClick={() => RejectLearning(l.id).then(done, errMsg)}>Reject</Button>
            <Button variant="secondary" tone="danger" title="Reject every proposed learning starting with the same 40 characters" onClick={() => RejectSimilarLearnings(l.id).then(done, errMsg)}>Reject similar</Button>
        </Group>
    )
}
