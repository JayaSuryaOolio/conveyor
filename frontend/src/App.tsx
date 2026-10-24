import {useEffect, useRef, useState} from 'react'
import {AppShell, Badge, Button, Kbd, NavLink, Stack, Text, TextInput, Title} from '@mantine/core'
import {IconBolt, IconBook2, IconClock, IconFolders, IconLayoutKanban, IconLogs, IconPlus, IconSearch, IconSettings} from '@tabler/icons-react'
import {DispatcherState, Learnings} from '../wailsjs/go/ui/App'
import {Automations} from './automations'
import {Nav} from './components'
import {NewTask, Repos, Settings} from './forms'
import {LearningsPage} from './learnings'
import {Logs} from './logs'
import {useData} from './util'
import {Board, Focus, TaskPage} from './views'

const NAV = [
    {name: 'board', label: 'Board', icon: IconLayoutKanban},
    {name: 'focus', label: 'Focus', icon: IconBolt},
    {name: 'learnings', label: 'Learnings', icon: IconBook2},
    {name: 'automations', label: 'Automations', icon: IconClock},
    {name: 'repos', label: 'Repos', icon: IconFolders},
    {name: 'logs', label: 'Logs', icon: IconLogs},
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
                if (e.key === 'b') setView({name: 'board'})
                if (e.key === 'f') setView({name: 'focus'})
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
                        {NAV.map(({name, label, icon: Icon}) => (
                            <NavLink key={name} label={label} active={view.name === name} leftSection={<Icon size={18}/>}
                                     rightSection={name === 'learnings' && proposed > 0 ? <Badge size="sm" circle>{proposed}</Badge> : undefined}
                                     onClick={() => setView({name})}/>
                        ))}
                    </Stack>
                    <Text mt="auto" size="xs" c="dimmed" title="Whether this app is running the dispatcher">{dispatcher}</Text>
                </Stack>
            </AppShell.Navbar>
            <AppShell.Main h="100vh" style={{overflow: 'auto'}}>
                {view.name === 'board' && <Board nav={nav} q={q}/>}
                {view.name === 'focus' && <Focus nav={nav} q={q}/>}
                {view.name === 'task' && <TaskPage k={view.key!} nav={nav}/>}
                {view.name === 'learnings' && <LearningsPage nav={nav}/>}
                {view.name === 'automations' && <Automations/>}
                {view.name === 'repos' && <Repos/>}
                {view.name === 'logs' && <Logs/>}
                {view.name === 'settings' && <Settings/>}
            </AppShell.Main>
            {showNew && <NewTask nav={nav} close={() => setShowNew(false)}/>}
        </AppShell>
    )
}
