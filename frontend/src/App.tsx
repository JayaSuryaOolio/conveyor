import {useEffect, useMemo, useRef, useState} from 'react'
import {useLocation, useNavigate} from 'react-router'
import {ActionIcon, AppShell, Badge, Box, Button, Group, Kbd, NavLink, Stack, Text, Title, Tooltip} from '@mantine/core'
import {useLocalStorage} from '@mantine/hooks'
import {IconApps, IconBolt, IconArrowLeft, IconBook2, IconClock, IconLayoutKanban, IconLayoutSidebarLeftCollapse, IconLayoutSidebarLeftExpand, IconLogs, IconPlus, IconRobot, IconSettings, IconSitemap} from '@tabler/icons-react'
import {AppVersion, DispatcherState, Learnings, ShellPATHError} from '../wailsjs/go/ui/App'
import {Agents} from './agents'
import {EpicView} from './epic'
import {Apps} from './apps'
import {Automations} from './automations'
import {Nav, PlacedButtons, Toasts, View, toast} from './components'
import {NewTask, Settings} from './forms'
import {LearningsPage} from './learnings'
import {Logs} from './logs'
import {CommandPalette} from './palette'
import {useData} from './util'
import {NavContext} from './markdown'
import {fromLocation, toPath} from './router'
import {Board, Focus, TaskPage} from './views'

// `label` on a view: the board filtered by that label ("Epics" = label:epic).
const NAV: {name: string; key?: string; label: string; icon: typeof IconBolt; filter?: string}[] = [
    {name: 'board', key: 'b', label: 'Board', icon: IconLayoutKanban},
    {name: 'board', label: 'Epics', icon: IconSitemap, filter: 'epic'},
    {name: 'learnings', label: 'Learnings', icon: IconBook2},
    {name: 'automations', label: 'Automations', icon: IconClock},
    {name: 'apps', label: 'Connectors', icon: IconApps},
    {name: 'agents', key: 'a', label: 'Agents', icon: IconRobot},
    {name: 'logs', key: 'l', label: 'Logs', icon: IconLogs},
    {name: 'settings', label: 'Settings', icon: IconSettings},
]

export default function App() {
    // History lives in react-router (hash history); views map to URLs in router.ts.
    const loc = useLocation()
    const navigate = useNavigate()
    const view = useMemo(() => fromLocation(loc), [loc]) // new object per navigation, so re-picking a board filter re-applies it
    const canBack = (window.history.state?.idx ?? 0) > 0
    const setView = (v: View) => { const to = toPath(v); if (to !== viewRef.current.path) navigate(to) }
    const back = () => { if (window.history.state?.idx > 0) navigate(-1) }
    const [showNew, setShowNew] = useState(false)
    const [showFocus, setShowFocus] = useState(false)
    const [q, setQ] = useState('')
    const search = useRef<HTMLInputElement>(null)
    const [dispatcher, setDispatcher] = useState('')
    const [mini, setMini] = useLocalStorage({key: 'nav-collapsed', defaultValue: false})
    const {data: learnings} = useData(Learnings, [])
    const proposed = learnings?.filter(l => l.status === 'proposed').length ?? 0
    const {data: version} = useData(AppVersion, [], {static: true})
    const nav: Nav = v => setView(v)
    const viewRef = useRef({view, path: ''})
    viewRef.current = {view, path: loc.pathname + loc.search}

    // No close button: without the login shell's PATH, runners, gh and hooks may not be found.
    useEffect(() => { ShellPATHError().then(e => { if (e) toast(`Could not load your shell's PATH, so claude, agy, gh or git hooks may not be found. Restart Conveyor after fixing your shell rc files.\n${e}`, true) }) }, [])

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
            if (e.key === 'Escape' && !e.defaultPrevented && el.matches('input,textarea,select')) { e.preventDefault(); el.blur(); return } // de-focus instead of macOS leaving fullscreen
            if (e.metaKey && e.key === '[') { e.preventDefault(); back(); return }
            if (el.matches('input,textarea,select') || e.metaKey || e.ctrlKey) return
            if (g) {
                g = false
                const n = NAV.find(x => x.key === e.key)
                if (n) setView({name: n.name})
                return
            }
            if (e.key === 'n') { e.preventDefault(); setShowNew(true) }
            else if (e.key === 'e' && viewRef.current.view.name === 'task') { e.preventDefault(); setView({name: 'epic', key: viewRef.current.view.key}) } // epic view of the open task's tree
            else if (e.key === '/') { e.preventDefault(); search.current?.focus() } // only the board mounts the input; refactor: move q+ref into Board
            else if (e.key === 'g') { g = true; setTimeout(() => (g = false), 1000) }
        }
        window.addEventListener('keydown', onKey)
        return () => window.removeEventListener('keydown', onKey)
    }, [])

    return (
        <NavContext.Provider value={nav}>
        <AppShell navbar={{width: mini ? 60 : 220, breakpoint: 0}} padding="md">
            <AppShell.Navbar p="sm">
                <Stack gap="xs" h="100%">
                    <Group justify={mini ? 'center' : 'space-between'} wrap="nowrap">
                        {!mini && <Group gap={4} wrap="nowrap">
                            <Tooltip label="Back (⌘[)" position="right">
                                <ActionIcon variant="subtle" color="gray" aria-label="Back" disabled={!canBack} onClick={back}><IconArrowLeft size={18}/></ActionIcon>
                            </Tooltip>
                            <Title order={4}>Conveyor</Title>
                        </Group>}
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
                    </>}
                    <Stack gap={2} mt="xs">
                        {NAV.map(({name, key, label, icon: Icon, filter}) => (
                            <Tooltip key={label} label={label} position="right" disabled={!mini}>
                                <NavLink label={mini ? undefined : label} active={view.name === name && (view.label ?? undefined) === filter} leftSection={<Icon size={18}/>} aria-label={label}
                                         rightSection={mini ? undefined : name === 'learnings' && proposed > 0 ? <Badge size="sm" circle>{proposed}</Badge> : key ? <Kbd size="xs" title={`g then ${key}`}>g {key}</Kbd> : undefined}
                                         onClick={() => setView(filter ? {name, label: filter} : {name})}/>
                            </Tooltip>
                        ))}
                    </Stack>
                    <Box mt="auto">{!mini && <PlacedButtons place="global_nav" size="compact-xs"/>}</Box>
                    <Group gap="xs" justify={mini ? 'center' : 'space-between'} wrap="nowrap">
                        <Tooltip label={dispatcher === 'dispatching' ? 'Dispatcher running in this app' : 'UI only: another process dispatches'} position="right">
                            <Box w={8} h={8} style={{borderRadius: '50%', background: `var(--mantine-color-${dispatcher === 'dispatching' ? 'green' : 'gray'}-6)`}}/>
                        </Tooltip>
                        {!mini && version && <Text size="xs" c="dimmed">{version}</Text>}
                    </Group>
                </Stack>
            </AppShell.Navbar>
            <AppShell.Main key={view.name + (view.key ?? '')} h="100vh" style={{overflow: view.name === 'task' ? 'hidden' : 'auto'}}>
                {view.name === 'board' && <Board nav={nav} q={q} setQ={setQ} search={search} link={view}/>}
                                {view.name === 'task' && <TaskPage k={view.key!} nav={nav}/>}
                {view.name === 'epic' && <EpicView k={view.key!} task={view.task} nav={nav}/>}
                {view.name === 'learnings' && <LearningsPage nav={nav}/>}
                {view.name === 'automations' && <Automations/>}
                {view.name === 'apps' && <Apps/>}
                {view.name === 'agents' && <Agents/>}
                {view.name === 'logs' && <Logs/>}
                {view.name === 'settings' && <Settings/>}
            </AppShell.Main>
            {showFocus && <Focus nav={nav} onClose={() => setShowFocus(false)}/>}
            {showNew && <NewTask nav={nav} close={() => setShowNew(false)}/>}
            <CommandPalette nav={nav} onFocus={() => setShowFocus(true)}/>
            <Toasts/>
        </AppShell>
        </NavContext.Provider>
    )
}
