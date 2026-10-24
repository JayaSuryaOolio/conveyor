import {Button, Code, Stack, Switch, Table, Text, Title} from '@mantine/core'
import {ListAutomations, RunAutomation, SetAutomationEnabled} from '../wailsjs/go/ui/App'
import {Time, errMsg} from './components'
import {useData} from './util'

export function Automations() {
    const {data, error, reload} = useData(ListAutomations, [])
    return (
        <Stack>
            <Title order={3}>Automations</Title>
            {error && <Text c="red">{error}</Text>}
            {data && data.length === 0 && <Text c="dimmed">None yet. Add one with <Code>conveyor automation add --template pr-sweep</Code>.</Text>}
            {data && data.length > 0 && (
                <Table.ScrollContainer minWidth={700}>
                    <Table verticalSpacing="xs">
                        <Table.Thead><Table.Tr><Table.Th>Enabled</Table.Th><Table.Th>Name</Table.Th><Table.Th>Schedule</Table.Th><Table.Th>Kind</Table.Th><Table.Th>Last run</Table.Th><Table.Th>Result</Table.Th><Table.Th/></Table.Tr></Table.Thead>
                        <Table.Tbody>
                            {data.map(a => (
                                <Table.Tr key={a.name}>
                                    <Table.Td><Switch checked={a.enabled} onChange={e => SetAutomationEnabled(a.name, e.currentTarget.checked).then(reload, errMsg)}/></Table.Td>
                                    <Table.Td fw={600}>{a.name}</Table.Td>
                                    <Table.Td><Code>{a.schedule}</Code></Table.Td>
                                    <Table.Td>{a.action_kind}</Table.Td>
                                    <Table.Td>{a.last_run_at ? <Time iso={a.last_run_at}/> : ''}</Table.Td>
                                    <Table.Td><Text size="sm" lineClamp={1}>{a.last_result ?? ''}</Text></Table.Td>
                                    <Table.Td><Button size="xs" variant="default" onClick={() => RunAutomation(a.name).then(reload, e => { errMsg(e); reload() })}>Run now</Button></Table.Td>
                                </Table.Tr>
                            ))}
                        </Table.Tbody>
                    </Table>
                </Table.ScrollContainer>
            )}
        </Stack>
    )
}
