import {lazy, Suspense, useRef, useState} from 'react'
import {ActionIcon, Group, Stack, Tabs, Text, Textarea, Tooltip, Typography} from '@mantine/core'
import {IconBold, IconCode, IconItalic, IconLink, IconList, IconListCheck} from '@tabler/icons-react'

const MdRender = lazy(() => import('./md-render'))

/** Renders GitHub-flavoured markdown; raw HTML stays escaped (react-markdown default). */
export function Markdown({children, c}: {children: string; c?: string}) {
    return (
        <Typography fz="sm" c={c} className="md" style={{wordBreak: 'break-word'}}>
            <Suspense fallback={<span style={{whiteSpace: 'pre-wrap'}}>{children}</span>}><MdRender>{children}</MdRender></Suspense>
        </Typography>
    )
}

type Edit = {value: string; start: number; end: number}
const LIST_LINE = /^\s*([-*+]|\d+\.)\s/

/** Write/Preview markdown editor with a small toolbar; Cmd+Enter calls onSubmit. */
export function MarkdownEditor({value, onChange, onSubmit, placeholder, minRows = 3}: {
    value: string; onChange: (v: string) => void; onSubmit?: () => void; placeholder?: string; minRows?: number
}) {
    const ref = useRef<HTMLTextAreaElement>(null)
    const [tab, setTab] = useState<string | null>('write')
    const apply = (fn: (v: string, s: number, e: number) => Edit) => {
        const t = ref.current
        if (!t) return
        const r = fn(t.value, t.selectionStart, t.selectionEnd)
        onChange(r.value)
        requestAnimationFrame(() => { t.focus(); t.setSelectionRange(r.start, r.end) })
    }
    const wrap = (l: string, r = l) => () => apply((v, s, e) => ({
        value: v.slice(0, s) + l + v.slice(s, e) + r + v.slice(e), start: s + l.length, end: e + l.length,
    }))
    const prefix = (p: string) => () => apply((v, s, e) => {
        const from = v.lastIndexOf('\n', s - 1) + 1
        const block = v.slice(from, e).split('\n').map(x => p + x).join('\n')
        return {value: v.slice(0, from) + block + v.slice(e), start: s + p.length, end: from + block.length}
    })
    const link = () => apply((v, s, e) => {
        const text = v.slice(s, e) || 'text'
        const u = s + text.length + 3
        return {value: v.slice(0, s) + '[' + text + '](url)' + v.slice(e), start: u, end: u + 3}
    })
    const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
        const mod = e.metaKey || e.ctrlKey
        if (mod && e.key === 'Enter') { e.preventDefault(); onSubmit?.() }
        else if (mod && e.key === 'b') { e.preventDefault(); wrap('**')() }
        else if (mod && e.key === 'i') { e.preventDefault(); wrap('_')() }
        else if (e.key === 'Tab' && !e.shiftKey && !mod) {
            const t = e.currentTarget
            const line = t.value.slice(t.value.lastIndexOf('\n', t.selectionStart - 1) + 1, t.selectionStart)
            if (!LIST_LINE.test(line)) return // not in a list: normal focus movement
            e.preventDefault()
            apply((v, s, en) => ({value: v.slice(0, s) + '  ' + v.slice(en), start: s + 2, end: s + 2}))
        }
    }
    const tools: [string, React.ReactNode, () => void][] = [
        ['Bold (⌘B)', <IconBold size={14}/>, wrap('**')], ['Italic (⌘I)', <IconItalic size={14}/>, wrap('_')],
        ['Code', <IconCode size={14}/>, wrap('`')], ['Link', <IconLink size={14}/>, link],
        ['Bulleted list', <IconList size={14}/>, prefix('- ')], ['Task list', <IconListCheck size={14}/>, prefix('- [ ] ')],
    ]
    return (
        <Tabs value={tab} onChange={setTab} variant="outline" keepMounted={false}>
            <Group justify="space-between" align="flex-end" wrap="nowrap">
                <Tabs.List>
                    <Tabs.Tab value="write">Write</Tabs.Tab>
                    <Tabs.Tab value="preview">Preview</Tabs.Tab>
                </Tabs.List>
                {tab === 'write' && (
                    <Group gap={2}>
                        {tools.map(([label, icon, fn]) => (
                            <Tooltip key={label} label={label}>
                                <ActionIcon variant="subtle" color="gray" size="sm" aria-label={label} onMouseDown={e => e.preventDefault()} onClick={fn}>{icon}</ActionIcon>
                            </Tooltip>
                        ))}
                    </Group>
                )}
            </Group>
            <Tabs.Panel value="write" pt="xs">
                <Textarea ref={ref} autosize minRows={minRows} maxRows={14} value={value} placeholder={placeholder}
                          onChange={e => onChange(e.target.value)} onKeyDown={onKeyDown}/>
            </Tabs.Panel>
            <Tabs.Panel value="preview" pt="xs">
                <Stack mih={minRows * 24} px="sm">
                    {value.trim() ? <Markdown>{value}</Markdown> : <Text size="sm" c="dimmed">Nothing to preview</Text>}
                </Stack>
            </Tabs.Panel>
        </Tabs>
    )
}
