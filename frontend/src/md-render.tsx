import Md from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {BrowserOpenURL} from '../wailsjs/runtime/runtime'

const plugins = [remarkGfm]
const components = {
    // Never navigate the webview: hand links to the system browser.
    a: ({href, children}: {href?: string; children?: React.ReactNode}) => (
        <a href={href} onClick={e => { e.preventDefault(); if (href) BrowserOpenURL(href) }}>{children}</a>
    ),
}

export default function MdRender({children}: {children: string}) {
    return <Md remarkPlugins={plugins} components={components}>{children}</Md>
}
