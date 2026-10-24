import {useEffect, useRef, useState} from 'react'
import {DispatcherState, Learnings} from '../wailsjs/go/ui/App'
import {Automations} from './automations'
import {Nav} from './components'
import {NewTask, Repos, Settings} from './forms'
import {LearningsPage} from './learnings'
import {useData} from './util'
import {Board, Focus, TaskPage} from './views'

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
        <>
            <header>
                <b>Conveyor</b>
                {['board', 'focus', 'learnings', 'automations', 'repos', 'settings'].map(n => (
                    <a key={n} href="#" className={view.name === n ? 'active' : ''}
                       onClick={e => { e.preventDefault(); setView({name: n}) }}>{n}{n === 'learnings' && proposed > 0 && <span className="chip">{proposed}</span>}</a>
                ))}
                <span className="muted badge" title="Whether this app is running the dispatcher">{dispatcher}</span>
                <input ref={search} className="search" placeholder="Search (/)" value={q} onChange={e => setQ(e.target.value)}/>
                <button onClick={() => setShowNew(true)}>New task (n)</button>
            </header>
            <main>
                {view.name === 'board' && <Board nav={nav} q={q}/>}
                {view.name === 'focus' && <Focus nav={nav} q={q}/>}
                {view.name === 'task' && <TaskPage k={view.key!} nav={nav}/>}
                {view.name === 'learnings' && <LearningsPage nav={nav}/>}
                {view.name === 'automations' && <Automations/>}
                {view.name === 'repos' && <Repos/>}
                {view.name === 'settings' && <Settings/>}
            </main>
            {showNew && <NewTask nav={nav} close={() => setShowNew(false)}/>}
        </>
    )
}
