import {useEffect, useRef, useState} from 'react'
import {AppShell, Badge, Button, Kbd, NavLink, Stack, Text, TextInput, Title} from '@mantine/core'
import {IconApps, IconBolt, IconBook2, IconClock, IconFolders, IconLayoutKanban, IconLogs, IconPlus, IconRobot, IconSearch, IconSettings} from '@tabler/icons-react'
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
            else if (e.key === '/') { e.preventDefault(); search.current?.focus() }
            else if (e.key === 'g') { g = true; setTimeout(() => (g = false), 1000) }
        }
        window.addEventListener('keydown', onKey)
        return () => window.removeEventListener('keydown', onKey)
    }, [])

    return (
        <AppShell navbar={{width: 220, breakpoint: 0}} padding="md">
            <AppShell.Navbar p="sm">
                <Stack gap="xs" h="100%">
                    <Title order={4}>Conveyor</Title>
                    <Button leftSection={<IconPlus size={16}/>} rightSection={<Kbd size="xs">n</Kbd>} onClick={() => setShowNew(true)}>New task</Button>
                    <TextInput ref={search} placeholder="Search" leftSection={<IconSearch size={16}/>} rightSection={<Kbd size="xs">/</Kbd>}
                               value={q} onChange={e => setQ(e.target.value)}/>
                    <Stack gap={2} mt="xs">
                        {NAV.map(({name, key, label, icon: Icon}) => (
                            <NavLink key={name} label={label} active={view.name === name} leftSection={<Icon size={18}/>}
                                     rightSection={name === 'learnings' && proposed > 0 ? <Badge size="sm" circle>{proposed}</Badge> : key ? <Kbd size="xs" title={`g then ${key}`}>g {key}</Kbd> : undefined}
                                     onClick={() => setView({name})}/>
                        ))}
                    </Stack>
                    <Text mt="auto" size="xs" c="dimmed" title="Whether this app is running the dispatcher">{dispatcher}</Text>
                </Stack>
            </AppShell.Navbar>
            <AppShell.Main key={view.name + (view.key ?? '')} h="100vh" style={{overflow: 'auto'}}>
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
