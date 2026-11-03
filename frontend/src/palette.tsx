import {useCallback, useEffect, useState} from 'react'
import {ActionIcon, Badge, Group, Text, Tooltip} from '@mantine/core'
import {Spotlight, spotlight} from '@mantine/spotlight'
import '@mantine/spotlight/styles.css'
import {IconFilter, IconLayoutKanban, IconRefresh, IconRobot, IconSearch, IconSettings} from '@tabler/icons-react'
import {Nav} from './components'
import {SETTING_LABELS} from './forms'
import {load, Raw} from './paletteCache'

type Item = {id: string; label: string; hint: string; go: () => void}

const PER_GROUP = 20

export function CommandPalette({nav}: {nav: Nav}) {
    const [raw, setRaw] = useState<Raw>()
    const [query, setQuery] = useState('')
    const [busy, setBusy] = useState(false)
    const refresh = useCallback((force: boolean) => {
        setBusy(true)
        load(force).then(setRaw, () => {}).finally(() => setBusy(false))
    }, [])
    useEffect(() => refresh(false), [refresh])
    useEffect(() => { // own listener: Mantine's `shortcut` skips focused inputs; capture so no field swallows it
        const onKey = (e: KeyboardEvent) => {
            if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); spotlight.toggle() }
        }
        window.addEventListener('keydown', onKey, true)
        return () => window.removeEventListener('keydown', onKey, true)
    }, [])

    const words = query.toLowerCase().split(/\s+/).filter(Boolean)
    const match = (s: string) => words.every(w => s.toLowerCase().includes(w))
    // Filter each source before building items, so a large board never allocates more than PER_GROUP rows.
    const pick = <T, >(xs: T[], text: (x: T) => string, item: (x: T) => Item) => {
        const out: Item[] = []
        for (const x of xs) {
            if (out.length >= PER_GROUP) break
            if (match(text(x))) out.push(item(x))
        }
        return out
    }
    const r = raw ?? {tasks: [], agents: [], labels: [], repos: []}
    const groups: {title: string; icon: typeof IconSearch; items: Item[]}[] = [
        {title: 'Tasks', icon: IconLayoutKanban, items: pick(r.tasks, t => `${t.key} ${t.title}`, t => ({id: 't' + t.key, label: `${t.key} ${t.title}`, hint: t.status, go: () => nav({name: 'task', key: t.key})}))},
        {title: 'Filters', icon: IconFilter, items: [
            ...pick(r.labels, l => 'label ' + l, l => ({id: 'l' + l, label: l, hint: 'label', go: () => nav({name: 'board', label: l})})),
            ...pick(r.repos, x => 'repo ' + x, x => ({id: 'r' + x, label: x, hint: 'repo', go: () => nav({name: 'board', repo: x})})),
        ]},
        {title: 'Agents', icon: IconRobot, items: pick(r.agents, a => a.name, a => ({id: 'a' + a.scope + a.name, label: a.name, hint: a.scope, go: () => nav({name: 'agents'})}))},
        {title: 'Settings', icon: IconSettings, items: pick(SETTING_LABELS, s => s, s => ({id: 's' + s, label: s, hint: '', go: () => nav({name: 'settings'})}))},
    ].filter(g => g.items.length)

    return (
        <Spotlight.Root query={query} onQueryChange={setQuery} scrollable maxHeight={420} onSpotlightClose={() => setQuery('')}>
            <Spotlight.Search placeholder="Search tasks, filters, agents and settings" leftSection={<IconSearch size={18}/>} rightSectionPointerEvents="all"
                              rightSection={<Tooltip label="Reload (ignore cache)"><ActionIcon variant="subtle" color="gray" aria-label="Reload" loading={busy} onClick={() => refresh(true)}><IconRefresh size={16}/></ActionIcon></Tooltip>}/>
            <Spotlight.ActionsList>
                {groups.length ? groups.map(({title, icon: Icon, items}) => (
                    <Spotlight.ActionsGroup key={title} label={title}>
                        {items.map(i => (
                            <Spotlight.Action key={i.id} onClick={i.go}>
                                <Group wrap="nowrap" w="100%">
                                    <Icon size={18}/>
                                    <Text flex={1} truncate>{i.label}</Text>
                                    {i.hint && <Badge variant="light" color="gray">{i.hint}</Badge>}
                                </Group>
                            </Spotlight.Action>
                        ))}
                    </Spotlight.ActionsGroup>
                )) : <Spotlight.Empty>Nothing found</Spotlight.Empty>}
            </Spotlight.ActionsList>
        </Spotlight.Root>
    )
}
