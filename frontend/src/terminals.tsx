import {useEffect, useRef, useState} from 'react'
import {ActionIcon, Group, Text, Tooltip, UnstyledButton} from '@mantine/core'
import {IconArrowsMaximize, IconArrowsMinimize, IconChevronDown, IconChevronUp, IconX} from '@tabler/icons-react'
import {FitAddon} from '@xterm/addon-fit'
import {Terminal} from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import {TermBuffer, TermClose, TermList, TermResize, TermWrite} from '../wailsjs/go/ui/App'
import {ui} from '../wailsjs/go/models'
import {EventsOn} from '../wailsjs/runtime/runtime'
import {errMsg} from './components'

const HEADER = 32
const H_KEY = 'conveyor.termHeight'
const bytes = (b64: string) => Uint8Array.from(atob(b64), c => c.charCodeAt(0))

/** One xterm bound to a Go terminal: replays the scrollback, then follows live output. */
function XTerm({id, active}: {id: string; active: boolean}) {
    const el = useRef<HTMLDivElement>(null)
    const term = useRef<Terminal | undefined>(undefined)
    useEffect(() => {
        const t = new Terminal({
            fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace', fontSize: 13, cursorBlink: true, scrollback: 5000,
            theme: {background: '#1a1b1e', foreground: '#c1c2c5', cursor: '#c1c2c5', selectionBackground: '#3b5bdb88'},
        })
        const fit = new FitAddon()
        t.loadAddon(fit)
        t.open(el.current!)
        term.current = t
        // Live chunks are "<end>:<base64>"; end is the byte count after the chunk, so
        // whatever the replay already covered is skipped.
        let base = 0, ready = false
        const queue: [number, Uint8Array][] = []
        const feed = (end: number, b: Uint8Array) => {
            if (end <= base) return
            const start = end - b.length
            t.write(start < base ? b.subarray(base - start) : b)
            base = end
        }
        const off = [
            EventsOn('term:' + id, (s: string) => {
                const i = s.indexOf(':'), end = Number(s.slice(0, i)), b = bytes(s.slice(i + 1))
                if (ready) feed(end, b); else queue.push([end, b])
            }),
            EventsOn('term-exit:' + id, () => t.write('\r\n\x1b[2m[process exited]\x1b[0m\r\n')),
        ]
        // Replay only after the first visible fit: a hidden xterm is 80 cols, and the
        // scrollback was drawn at the PTY's real width, so replaying early garbles it.
        let replayed = false
        const replay = () => TermBuffer(id).then(buf => {
            const b = bytes(buf.data)
            t.write(b)
            base = buf.end
            queue.forEach(([end, c]) => feed(end, c))
            ready = true
        }, errMsg)
        t.onData(d => TermWrite(id, d).catch(() => {}))
        // xterm sends a bare \r for Shift+Enter; send ESC+CR like Claude's /terminal-setup so TUIs insert a newline.
        t.attachCustomKeyEventHandler(e => {
            if (e.key !== 'Enter' || !e.shiftKey || e.ctrlKey || e.altKey || e.metaKey) return true
            if (e.type === 'keydown') TermWrite(id, '\x1b\r').catch(() => {})
            return false
        })
        const ro = new ResizeObserver(() => {
            if (!el.current?.offsetWidth) return // hidden tab
            fit.fit()
            TermResize(id, t.cols, t.rows).catch(() => {})
            if (!replayed) { replayed = true; replay() }
        })
        ro.observe(el.current!)
        return () => { ro.disconnect(); off.forEach(f => f()); t.dispose() }
    }, [id])
    useEffect(() => { if (active) term.current?.focus() }, [active])
    return <div ref={el} style={{display: active ? 'block' : 'none', height: '100%', padding: '4px 0 0 8px', background: '#1a1b1e'}}/>
}

/** Bottom panel of a task page: one tab per terminal. `added` is a freshly started one. */
export default function TermDock({k, added}: {k: string; added?: ui.TermInfo}) {
    const [tabs, setTabs] = useState<ui.TermInfo[]>([])
    const [active, setActive] = useState('')
    const [open, setOpen] = useState(false)
    const [max, setMax] = useState(false)
    const [h, setH] = useState(() => { try { return Number(localStorage.getItem(H_KEY)) || 300 } catch { return 300 } })
    useEffect(() => {
        TermList(k).then(l => { setTabs(l); setActive(l[0]?.id ?? '') }, errMsg)
    }, [k])
    useEffect(() => {
        if (!added) return
        setTabs(t => (t.some(x => x.id === added.id) ? t : [...t, added]))
        setActive(added.id)
        setOpen(true)
    }, [added])
    const close = (id: string) => TermClose(k, id).catch(errMsg).then(() => {
        setTabs(t => {
            const rest = t.filter(x => x.id !== id)
            if (id === active) setActive(rest[rest.length - 1]?.id ?? '')
            return rest
        })
    })
    const drag = (e: React.MouseEvent) => {
        e.preventDefault()
        let last = h
        const move = (m: MouseEvent) => { last = Math.min(Math.max(window.innerHeight - m.clientY, 120), window.innerHeight - 80); setH(last) }
        const up = () => {
            window.removeEventListener('mousemove', move)
            window.removeEventListener('mouseup', up)
            try { localStorage.setItem(H_KEY, String(last)) } catch { /* private mode */ }
        }
        window.addEventListener('mousemove', move)
        window.addEventListener('mouseup', up)
    }
    const height = !open ? HEADER : max ? 'calc(100vh - 60px)' : h
    return (
        <div style={{position: 'sticky', bottom: 0, zIndex: 10, height, display: 'flex', flexDirection: 'column', background: 'var(--mantine-color-body)', borderTop: '1px solid var(--mantine-color-default-border)'}}>
            {open && !max && <div onMouseDown={drag} style={{position: 'absolute', top: -3, left: 0, right: 0, height: 6, cursor: 'ns-resize', zIndex: 1}}/>}
            <Group gap={4} px="xs" h={HEADER} wrap="nowrap" style={{flex: 'none'}} justify="space-between">
                <Group gap={2} wrap="nowrap" style={{overflowX: 'auto'}}>
                    {tabs.length === 0 && <Text size="xs" c="dimmed">Terminals</Text>}
                    {tabs.map(t => (
                        <Group key={t.id} gap={2} wrap="nowrap" px={6} style={{borderRadius: 4, background: t.id === active && open ? 'var(--mantine-color-default-hover)' : undefined}}>
                            <UnstyledButton onClick={() => { setActive(t.id); setOpen(true) }}>
                                <Text size="xs" fw={t.id === active ? 600 : 400} style={{whiteSpace: 'nowrap'}}>{t.title}</Text>
                            </UnstyledButton>
                            <ActionIcon variant="subtle" color="gray" size="xs" aria-label="Close terminal" onClick={() => close(t.id)}><IconX size={12}/></ActionIcon>
                        </Group>
                    ))}
                </Group>
                <Group gap={2} wrap="nowrap">
                    {open && (
                        <Tooltip label={max ? 'Restore' : 'Maximize'}>
                            <ActionIcon variant="subtle" color="gray" size="sm" aria-label="Maximize" onClick={() => setMax(!max)}>{max ? <IconArrowsMinimize size={14}/> : <IconArrowsMaximize size={14}/>}</ActionIcon>
                        </Tooltip>
                    )}
                    <Tooltip label={open ? 'Collapse' : 'Expand'}>
                        <ActionIcon variant="subtle" color="gray" size="sm" aria-label="Toggle terminals" onClick={() => setOpen(!open)}>{open ? <IconChevronDown size={14}/> : <IconChevronUp size={14}/>}</ActionIcon>
                    </Tooltip>
                </Group>
            </Group>
            <div style={{flex: 1, minHeight: 0, display: open ? 'block' : 'none'}}>
                {tabs.map(t => <XTerm key={t.id} id={t.id} active={t.id === active}/>)}
            </div>
        </div>
    )
}
