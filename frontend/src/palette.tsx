import {useCallback, useEffect, useState} from 'react'
import {ActionIcon, Badge, Group, Text, Tooltip} from '@mantine/core'
import {Spotlight} from '@mantine/spotlight'
import '@mantine/spotlight/styles.css'
import {IconLayoutKanban, IconRefresh, IconRobot, IconSearch, IconSettings} from '@tabler/icons-react'
import {ListAgents, ListRepos, ListTasks} from '../wailsjs/go/ui/App'
import {Nav} from './components'
import {SETTING_LABELS} from './forms'

type Item = {id: string; group: 'Tasks' | 'Agents' | 'Settings'; label: string; hint: string; go: () => void}

const CACHE_KEY = 'palette-cache'
const TTL = 24 * 3600 * 1000
type Raw = {tasks: {key: string; title: string; status: string}[]; agents: {name: string; scope: string; description: string}[]}

/** Board items and agents are fetched once a day (or on the reload button); settings are static. */
async function load(force: boolean): Promise<Raw> {
    try {
        const c = JSON.parse(localStorage.getItem(CACHE_KEY) ?? 'null')
        if (!force && c && Date.now() - c.at < TTL) return c.raw
    } catch {}
    const repos = (await ListRepos()).map(r => r.name)
    const [tasks, agents] = await Promise.all([ListTasks(), ListAgents(repos)])
    const raw: Raw = {
        tasks: tasks.map(t => ({key: t.key, title: t.title, status: t.status})),
        agents: agents.map(a => ({name: a.name, scope: a.scope, description: a.description})),
    }
    try { localStorage.setItem(CACHE_KEY, JSON.stringify({at: Date.now(), raw})) } catch {}
    return raw
}

const ICON = {Tasks: IconLayoutKanban, Agents: IconRobot, Settings: IconSettings}

export function CommandPalette({nav}: {nav: Nav}) {
    const [raw, setRaw] = useState<Raw>()
    const [query, setQuery] = useState('')
    const [busy, setBusy] = useState(false)
    const refresh = useCallback((force: boolean) => {
        setBusy(true)
        load(force).then(setRaw, () => {}).finally(() => setBusy(false))
    }, [])
    useEffect(() => refresh(false), [refresh])

    const items: Item[] = [
        ...(raw?.tasks ?? []).map((t): Item => ({id: 't' + t.key, group: 'Tasks', label: `${t.key} ${t.title}`, hint: t.status, go: () => nav({name: 'task', key: t.key})})),
        ...(raw?.agents ?? []).map((a): Item => ({id: 'a' + a.scope + a.name, group: 'Agents', label: a.name, hint: a.scope, go: () => nav({name: 'agents'})})),
        ...SETTING_LABELS.map((s): Item => ({id: 's' + s, group: 'Settings', label: s, hint: '', go: () => nav({name: 'settings'})})),
    ]
    const words = query.toLowerCase().split(/\s+/).filter(Boolean)
    const shown = items.filter(i => words.every(w => i.label.toLowerCase().includes(w))).slice(0, 50)

    return (
        <Spotlight.Root query={query} onQueryChange={setQuery} shortcut="mod + K" scrollable maxHeight={420} onSpotlightClose={() => setQuery('')}>
            <Spotlight.Search placeholder="Search tasks, agents and settings" leftSection={<IconSearch size={18}/>} rightSectionPointerEvents="all"
                              rightSection={<Tooltip label="Reload (ignore cache)"><ActionIcon variant="subtle" color="gray" aria-label="Reload" loading={busy} onClick={() => refresh(true)}><IconRefresh size={16}/></ActionIcon></Tooltip>}/>
            <Spotlight.ActionsList>
                {shown.length ? shown.map(i => {
                    const Icon = ICON[i.group]
                    return (
                        <Spotlight.Action key={i.id} onClick={i.go}>
                            <Group wrap="nowrap" w="100%">
                                <Icon size={18}/>
                                <Text flex={1} truncate>{i.label}</Text>
                                {i.hint && <Badge variant="light" color="gray">{i.hint}</Badge>}
                                <Text size="xs" c="dimmed">{i.group}</Text>
                            </Group>
                        </Spotlight.Action>
                    )
                }) : <Spotlight.Empty>Nothing found</Spotlight.Empty>}
            </Spotlight.ActionsList>
        </Spotlight.Root>
    )
}
