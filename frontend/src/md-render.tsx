import {useContext, useEffect, useMemo, useState} from 'react'
import {Menu, Modal, Tooltip} from '@mantine/core'
import Md from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {FileOpeners, Home, OpenFile, ShowError} from '../wailsjs/go/ui/App'
import {NavContext, TaskKey} from './markdown'
import {escapeRe, load, useKeyPrefix} from './paletteCache'
import {BrowserOpenURL} from '../wailsjs/runtime/runtime'

let homeP: Promise<string> | undefined
const useHome = () => {
    const [home, setHome] = useState('')
    useEffect(() => { (homeP ??= Home()).then(setHome, () => { homeP = undefined }) }, [])
    return home
}

/** Absolute paths under the conveyor home are served by the asset server at /attachments/<path>. */
function Img({src, alt}: {src?: string; alt?: string}) {
    const home = useHome()
    const [open, setOpen] = useState(false)
    const url = src && home && src.startsWith(home + '/') ? '/attachments' + src : src
    return (
        <>
            <img src={url} alt={alt} style={{maxWidth: '100%', cursor: 'zoom-in'}} onClick={() => setOpen(true)}/>
            <Modal opened={open} onClose={() => setOpen(false)} size="auto" centered withCloseButton={false}>
                <img src={url} alt={alt} style={{maxWidth: '100%', display: 'block'}}/>
            </Modal>
        </>
    )
}

/** @file and /command tokens (after a space or line start), as typed in the editor's autocomplete. */
const REF = /(^|\s)(@[\w./-]*[\w/-]|\/[a-z][\w:-]*)(?=$|[\s,;:!?)]|\.(?:\s|$))/g


type Node = {type: string; value?: string; children?: Node[]; data?: object}
/** Wraps `re` matches (group 1 = leading context to keep, group 2 = token) in text nodes (not in code or links) in <span class=cls(token)>. */
const wrapText = (re: RegExp, cls: (tok: string) => string) => () => (tree: Node) => {
    const walk = (n: Node) => {
        if (!n.children || n.type === 'link' || n.type === 'linkReference') return
        n.children = n.children.flatMap(c => {
            if (c.type !== 'text') { walk(c); return [c] }
            const out: Node[] = []
            const v = c.value ?? ''
            let at = 0
            for (const m of v.matchAll(re)) {
                const start = m.index! + m[1].length
                if (start > at) out.push({type: 'text', value: v.slice(at, start)})
                out.push({type: 'ref', data: {hName: 'span', hProperties: {className: cls(m[2])}}, children: [{type: 'text', value: m[2]}]})
                at = start + m[2].length
            }
            if (!out.length) return [c]
            if (at < v.length) out.push({type: 'text', value: v.slice(at)})
            return out
        })
    }
    walk(tree)
}
const remarkRefs = wrapText(REF, tok => 'md-ref md-ref-' + (tok[0] === '@' ? 'file' : 'cmd'))
/** Task keys with the key_prefix setting (e.g. CV-12A1); empty group 1 matches wrapText's (prefix)(token) shape. */
const remarkTaskKeys = (keyPrefix: string) => wrapText(new RegExp(`()(\\b${escapeRe(keyPrefix)}[0-9A-Z]+\\b)`, 'g'), () => 'md-ref md-ref-task')

/** An @file mention inside a task: click to open it in a configured editor or reveal it in Finder. */
function FileRef({children}: {children?: React.ReactNode}) {
    const key = useContext(TaskKey)
    const [apps, setApps] = useState<string[]>()
    const [err, setErr] = useState('')
    const ref = String(children)
    if (!key) return <span className="md-ref md-ref-file">{children}</span>
    return (
        <Menu position="bottom-start" withinPortal onOpen={() => FileOpeners(key, ref).then(a => { setApps(a); setErr('') }, e => { setApps([]); setErr(String(e)) })}>
            <Menu.Target><a href="#" className="md-ref md-ref-file" onClick={e => e.preventDefault()}>{children}</a></Menu.Target>
            <Menu.Dropdown>
                <Menu.Label>{err || 'Open ' + ref}</Menu.Label>
                {apps?.map(a => (
                    <Menu.Item key={a} onClick={() => OpenFile(key, ref, a).catch(e => ShowError(String(e)))}>{a === 'Finder' ? 'Reveal in Finder' : a}</Menu.Item>
                ))}
            </Menu.Dropdown>
        </Menu>
    )
}

/** A task key: a link to the task with its title as tooltip when the key is known; plain text otherwise (unknown key, cache not loaded). */
function TaskRef({children}: {children?: React.ReactNode}) {
    const nav = useContext(NavContext)
    const key = String(children)
    const [title, setTitle] = useState<string>()
    useEffect(() => { load(false).then(r => setTitle(r.titles?.[key]), () => {}) }, [key])
    if (!title) return <span>{children}</span>
    return (
        <Tooltip label={title} withinPortal>
            <a href="#" className="md-ref md-ref-task" onClick={e => { e.preventDefault(); nav({name: 'task', key}) }}>{children}</a>
        </Tooltip>
    )
}

const components = {
    // Never navigate the webview: hand links to the system browser.
    a: ({href, children}: {href?: string; children?: React.ReactNode}) => (
        <a href={href} onClick={e => { e.preventDefault(); if (href) BrowserOpenURL(href) }}>{children}</a>
    ),
    img: Img,
    span: ({className, children}: {className?: string; children?: React.ReactNode}) =>
        className === 'md-ref md-ref-file' ? <FileRef>{children}</FileRef>
            : className === 'md-ref md-ref-task' ? <TaskRef>{children}</TaskRef>
            : <span className={className}>{children}</span>,
}

export default function MdRender({children}: {children: string}) {
    const keyPrefix = useKeyPrefix()
    const plugins = useMemo(() => keyPrefix ? [remarkGfm, remarkRefs, remarkTaskKeys(keyPrefix)] : [remarkGfm, remarkRefs], [keyPrefix])
    return <Md remarkPlugins={plugins} components={components}>{children}</Md>
}
