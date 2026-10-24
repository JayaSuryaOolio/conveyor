import {useEffect, useState} from 'react'
import {Modal} from '@mantine/core'
import Md from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {Home} from '../wailsjs/go/ui/App'
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
/** Wraps REF matches in text nodes (not in code or links) in <span class="md-ref md-ref-file|cmd">. */
const remarkRefs = () => (tree: Node) => {
    const walk = (n: Node) => {
        if (!n.children || n.type === 'link' || n.type === 'linkReference') return
        n.children = n.children.flatMap(c => {
            if (c.type !== 'text') { walk(c); return [c] }
            const out: Node[] = []
            const v = c.value ?? ''
            let at = 0
            for (const m of v.matchAll(REF)) {
                const start = m.index! + m[1].length
                if (start > at) out.push({type: 'text', value: v.slice(at, start)})
                const cls = 'md-ref md-ref-' + (m[2][0] === '@' ? 'file' : 'cmd')
                out.push({type: 'ref', data: {hName: 'span', hProperties: {className: cls}}, children: [{type: 'text', value: m[2]}]})
                at = start + m[2].length
            }
            if (!out.length) return [c]
            if (at < v.length) out.push({type: 'text', value: v.slice(at)})
            return out
        })
    }
    walk(tree)
}

const plugins = [remarkGfm, remarkRefs]
const components = {
    // Never navigate the webview: hand links to the system browser.
    a: ({href, children}: {href?: string; children?: React.ReactNode}) => (
        <a href={href} onClick={e => { e.preventDefault(); if (href) BrowserOpenURL(href) }}>{children}</a>
    ),
    img: Img,
}

export default function MdRender({children}: {children: string}) {
    return <Md remarkPlugins={plugins} components={components}>{children}</Md>
}
