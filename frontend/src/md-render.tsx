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

const plugins = [remarkGfm]
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
