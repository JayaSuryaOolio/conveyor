import {useEffect, useRef, useState} from 'react'
import {ActionIcon, AppShell, Badge, Box, Button, Group, Kbd, NavLink, Stack, TextInput, Title, Tooltip} from '@mantine/core'
import {useLocalStorage} from '@mantine/hooks'
import {IconApps, IconBolt, IconBook2, IconClock, IconFolders, IconLayoutKanban, IconLayoutSidebarLeftCollapse, IconLayoutSidebarLeftExpand, IconLogs, IconPlus, IconRobot, IconSearch, IconSettings} from '@tabler/icons-react'
import {DispatcherState, Learnings} from '../wailsjs/go/ui/App'
import {Agents} from './agents'
import {Apps} from './apps'
import {Automations} from './automations'
import {Nav} from './components'
import {NewTask, Repos, Settings} from './forms'
import {LearningsPage} from './learnings'
import {Logs} from './logs'
import {useData} from './util'
import {Board, Focus, TaskPage} from './views'

const NAV: {name: string; key?: string; label: string; icon: typeof IconBolt}[] = [
    {name: 'board', key: 'b', label: 'Board', icon: IconLayoutKanban},
    {name: 'focus', key: 'f', label: 'Focus', icon: IconBolt},
    {name: 'learnings', label: 'Learnings', icon: IconBook2},
    {name: 'automations', label: 'Automations', icon: IconClock},
    {name: 'repos', label: 'Repos', icon: IconFolders},
    {name: 'apps', label: 'Apps', icon: IconApps},
    {name: 'agents', key: 'a', label: 'Agents', icon: IconRobot},
    {name: 'logs', key: 'l', label: 'Logs', icon: IconLogs},
    {name: 'settings', label: 'Settings', icon: IconSettings},
]

type View = {name: string; key?: string}

export default function App() {
    const [view, setView] = useState<View>({name: 'board'})
    const [showNew, setShowNew] = useState(false)
    const [q, setQ] = useState('')
    const search = useRef<HTMLInputElement>(null)
    const [dispatcher, setDispatcher] = useState('')
    const [mini, setMini] = useLocalStorage({key: 'nav-collapsed', defaultValue: false})
    const {data: learnings} = useData(Learnings, [])
    const proposed = learnings?.filter(l => l.status === 'proposed').length ?? 0
    const nav: Nav = v => setView(v)

    useEffect(() => {
        const poll = () => DispatcherState().then(setDispatcher, () => {})
        poll()
        const t = setInterval(poll, 5000)
        return () => clearInterval(t)
    }, [])

    useEffect(() => {
        let g = false
        const onKey = (e: KeyboardEvent) => {
            const el = e.target as HTMLElement
            if (el.matches('input,textarea,select') || e.metaKey || e.ctrlKey) return
            if (g) {
                g = false
                const n = NAV.find(x => x.key === e.key)
                if (n) setView({name: n.name})
                return
            }
            if (e.key === 'n') { e.preventDefault(); setShowNew(true) }
            else if (e.key === '/') { e.preventDefault(); setMini(false); setTimeout(() => search.current?.focus()) }
            else if (e.key === 'g') { g = true; setTimeout(() => (g = false), 1000) }
        }
        window.addEventListener('keydown', onKey)
        return () => window.removeEventListener('keydown', onKey)
    }, [])

    return (
        <AppShell navbar={{width: mini ? 60 : 220, breakpoint: 0}} padding="md">
            <AppShell.Navbar p="sm">
                <Stack gap="xs" h="100%">
                    <Group justify={mini ? 'center' : 'space-between'} wrap="nowrap">
                        {!mini && <Title order={4}>Conveyor</Title>}
                        <ActionIcon variant="subtle" color="gray" aria-label={mini ? 'Expand menu' : 'Collapse menu'} onClick={() => setMini(!mini)}>
                            {mini ? <IconLayoutSidebarLeftExpand size={18}/> : <IconLayoutSidebarLeftCollapse size={18}/>}
                        </ActionIcon>
                    </Group>
                    {mini ? (
                        <Tooltip label="New task (n)" position="right">
                            <ActionIcon size="lg" mx="auto" aria-label="New task" onClick={() => setShowNew(true)}><IconPlus size={18}/></ActionIcon>
                        </Tooltip>
                    ) : <>
                        <Button leftSection={<IconPlus size={16}/>} rightSection={<Kbd size="xs">n</Kbd>} onClick={() => setShowNew(true)}>New task</Button>
                        <TextInput ref={search} placeholder="Search" leftSection={<IconSearch size={16}/>} rightSection={<Kbd size="xs">/</Kbd>}
                                   value={q} onChange={e => setQ(e.target.value)}/>
                    </>}
                    <Stack gap={2} mt="xs">
                        {NAV.map(({name, key, label, icon: Icon}) => (
                            <Tooltip key={name} label={label} position="right" disabled={!mini}>
                                <NavLink label={mini ? undefined : label} active={view.name === name} leftSection={<Icon size={18}/>} aria-label={label}
                                         rightSection={mini ? undefined : name === 'learnings' && proposed > 0 ? <Badge size="sm" circle>{proposed}</Badge> : key ? <Kbd size="xs" title={`g then ${key}`}>g {key}</Kbd> : undefined}
                                         onClick={() => setView({name})}/>
                            </Tooltip>
                        ))}
                    </Stack>
                    <Tooltip label={dispatcher === 'dispatching' ? 'Dispatcher running in this app' : 'UI only: another process dispatches'} position="right">
                        <Box mt="auto" mx={mini ? 'auto' : 0} w={8} h={8} style={{borderRadius: '50%', background: `var(--mantine-color-${dispatcher === 'dispatching' ? 'green' : 'gray'}-6)`}}/>
                    </Tooltip>
                </Stack>
            </AppShell.Navbar>
            <AppShell.Main key={view.name + (view.key ?? '')} h="100vh" style={{overflow: view.name === 'task' ? 'hidden' : 'auto'}}>
                {view.name === 'board' && <Board nav={nav} q={q}/>}
                {view.name === 'focus' && <Focus nav={nav} q={q}/>}
                {view.name === 'task' && <TaskPage k={view.key!} nav={nav}/>}
                {view.name === 'learnings' && <LearningsPage nav={nav}/>}
                {view.name === 'automations' && <Automations/>}
                {view.name === 'repos' && <Repos/>}
                {view.name === 'apps' && <Apps/>}
                {view.name === 'agents' && <Agents/>}
                {view.name === 'logs' && <Logs/>}
                {view.name === 'settings' && <Settings/>}
            </AppShell.Main>
            {showNew && <NewTask nav={nav} close={() => setShowNew(false)}/>}
        </AppShell>
    )
}
