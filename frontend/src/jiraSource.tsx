import {useEffect, useState} from 'react'
import {Alert, Badge, Button, Collapse, Group, NumberInput, Paper, Stack, Text, TextInput, Textarea} from '@mantine/core'
import {JiraSourceClear, JiraSourceGet, JiraSourceSet, JiraSourceSyncNow} from '../wailsjs/go/ui/App'
import {store} from '../wailsjs/go/models'
import {errMsg} from './components'
import {useData} from './util'

/** Jira epic source of an epic: key, default repo, poll interval, status-map override, last sync/error, Sync now. */
export function JiraSource({epic}: {epic: string}) {
    const {data: src, reload} = useData(() => JiraSourceGet(epic), [epic])
    const [open, setOpen] = useState(false)
    const [f, setF] = useState({key: '', repo: '', interval: 300, map: ''})
    const [busy, setBusy] = useState(false)
    useEffect(() => setF({key: src?.jira_key ?? '', repo: src?.default_repo ?? '', interval: src?.interval_sec ?? 300, map: src?.status_map ?? ''}), [src])

    const save = () => JiraSourceSet(epic, store.JiraSource.createFrom({jira_key: f.key.trim(), default_repo: f.repo.trim(), interval_sec: f.interval, status_map: f.map.trim()})).then(reload, errMsg)
    const sync = () => { setBusy(true); JiraSourceSyncNow(epic).catch(errMsg).finally(() => { setBusy(false); reload() }) }
    return <Paper withBorder radius="md" px="sm" py={6}>
        <Group justify="space-between">
            <Group gap="xs">
                <Text size="sm" fw={600}>Jira source</Text>
                {src ? <Badge variant="light">{src.jira_key}{src.done ? ' · done' : ''}</Badge> : <Text size="sm" c="dimmed">none</Text>}
                {src && <Text size="xs" c="dimmed">{src.last_sync ? `synced ${new Date(src.last_sync).toLocaleString()}` : 'never synced'}</Text>}
            </Group>
            <Group gap="xs">
                {src && <Button size="compact-sm" loading={busy} onClick={sync}>Sync now</Button>}
                <Button size="compact-sm" variant="default" onClick={() => setOpen(o => !o)}>{open ? 'Hide' : src ? 'Edit' : 'Attach'}</Button>
            </Group>
        </Group>
        {src?.last_error && <Alert color="red" variant="light" mt={6} p="xs">{src.last_error}</Alert>}
        <Collapse expanded={open}>
            <Stack gap="xs" mt="xs">
                <Group grow align="flex-start">
                    <TextInput label="Jira epic key" placeholder="PROJ-123" value={f.key} onChange={e => setF({...f, key: e.currentTarget.value})}/>
                    <TextInput label="Default repo" value={f.repo} onChange={e => setF({...f, repo: e.currentTarget.value})}/>
                    <NumberInput label="Interval (s)" min={30} value={f.interval} onChange={v => setF({...f, interval: Number(v) || 300})}/>
                </Group>
                <Textarea label="Status map override (JSON)" placeholder='{"review": "In Review"}' autosize minRows={1} value={f.map} onChange={e => setF({...f, map: e.currentTarget.value})}/>
                <Group justify="flex-end">
                    {src && <Button size="compact-sm" color="red" variant="subtle" onClick={() => JiraSourceClear(epic).then(reload, errMsg)}>Detach</Button>}
                    <Button size="compact-sm" disabled={!f.key.trim()} onClick={save}>Save</Button>
                </Group>
            </Stack>
        </Collapse>
    </Paper>
}
