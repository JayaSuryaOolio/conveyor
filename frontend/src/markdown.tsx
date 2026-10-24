import {lazy, Suspense, useRef, useState} from 'react'
import {ActionIcon, Box, Button, Group, SegmentedControl, Stack, Text, Textarea, Tooltip, Typography} from '@mantine/core'
import {IconBold, IconCode, IconItalic, IconLink, IconList, IconListCheck} from '@tabler/icons-react'
import {SaveAttachment, ShowError} from '../wailsjs/go/ui/App'

const MdRender = lazy(() => import('./md-render'))

const toBase64 = (f: File) => new Promise<string>((ok, no) => {
    const r = new FileReader()
    r.onload = () => ok(String(r.result).split(',')[1] ?? '')
    r.onerror = () => no(r.error)
    r.readAsDataURL(f)
})

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

let uploads = 0

/**
 * GitHub-style comment box: Write/Preview + toolbar header, textarea, hint/submit footer.
 * Cmd+Enter calls onSubmit; pasted or dropped images are saved via SaveAttachment
 * (attachKey = task key, empty = shared folder) and inserted as markdown.
 */
export function MarkdownEditor({value, onChange, onSubmit, submitLabel = 'Send', submitting, canSubmit = true, attachKey = '', placeholder, minRows = 3}: {
    value: string; onChange: (v: string) => void; onSubmit?: () => void; submitLabel?: string; submitting?: boolean
    canSubmit?: boolean; attachKey?: string; placeholder?: string; minRows?: number
}) {
    const ref = useRef<HTMLTextAreaElement>(null)
    const cur = useRef(value)
    cur.current = value
    const [tab, setTab] = useState('write')
    const [focus, setFocus] = useState(false)
    const [over, setOver] = useState(false)
    const [pending, setPending] = useState(0)
    const submit = pending ? undefined : onSubmit
    const upload = (files: File[]) => files.filter(f => f.type.startsWith('image/')).forEach(f => {
        setPending(n => n + 1)
        const tok = `![Uploading…](#${++uploads})`
        const t = ref.current
        const at = t ? t.selectionStart : cur.current.length
        onChange(cur.current.slice(0, at) + tok + cur.current.slice(at))
        const done = (s: string) => onChange(cur.current.replace(tok, s))
        toBase64(f).then(b => SaveAttachment(attachKey, f.name || 'image.' + f.type.split('/')[1], b))
            .then(p => done(`![image](${p})`), e => { done(''); ShowError(String(e)) })
            .finally(() => setPending(n => n - 1))
    })
    const onPaste = (e: React.ClipboardEvent) => {
        const files = [...e.clipboardData.items].filter(i => i.type.startsWith('image/')).flatMap(i => i.getAsFile() ?? [])
        if (files.length) { e.preventDefault(); upload(files) }
    }
    const onDrop = (e: React.DragEvent) => {
        setOver(false)
        if (!e.dataTransfer.files.length) return
        e.preventDefault()
        upload([...e.dataTransfer.files])
    }
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
        if (mod && e.key === 'Enter') { e.preventDefault(); submit?.() }
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
        <Box
            onFocus={() => setFocus(true)} onBlur={() => setFocus(false)}
            onDragOver={e => { if (tab === 'write' && e.dataTransfer.types.includes('Files')) { e.preventDefault(); setOver(true) } }}
            onDragLeave={e => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setOver(false) }}
            onDrop={tab === 'write' ? onDrop : undefined}
            style={{
                border: '1px solid ' + (over || focus ? 'var(--mantine-primary-color-filled)' : 'var(--mantine-color-default-border)'),
                borderRadius: 'var(--mantine-radius-md)', overflow: 'hidden', width: '100%',
                boxShadow: focus ? '0 0 0 2px var(--mantine-primary-color-light)' : undefined,
                background: over ? 'var(--mantine-primary-color-light)' : undefined,
            }}>
            <Group justify="space-between" wrap="nowrap" px="xs" py={6} bg="var(--mantine-color-default-hover)"
                   style={{borderBottom: '1px solid var(--mantine-color-default-border)'}}>
                <SegmentedControl size="xs" value={tab} onChange={setTab} data={[{value: 'write', label: 'Write'}, {value: 'preview', label: 'Preview'}]}/>
                {tab === 'write' && (
                    <Group gap={2} wrap="nowrap">
                        {tools.map(([label, icon, fn]) => (
                            <Tooltip key={label} label={label}>
                                <ActionIcon variant="subtle" color="gray" size="sm" aria-label={label} onMouseDown={e => e.preventDefault()} onClick={fn}>{icon}</ActionIcon>
                            </Tooltip>
                        ))}
                    </Group>
                )}
            </Group>
            {tab === 'write'
                ? <Textarea ref={ref} variant="unstyled" px="sm" py={4} autosize minRows={minRows} maxRows={14} value={value} placeholder={placeholder}
                            onChange={e => onChange(e.target.value)} onKeyDown={onKeyDown} onPaste={onPaste}/>
                : <Stack mih={minRows * 24} p="sm">
                    {value.trim() ? <Markdown>{value}</Markdown> : <Text size="sm" c="dimmed">Nothing to preview</Text>}
                </Stack>}
            <Group justify="space-between" wrap="nowrap" px="xs" py={6} style={{borderTop: '1px solid var(--mantine-color-default-border)'}}>
                <Text size="xs" c="dimmed">{pending ? 'Uploading…' : <>Markdown supported · paste or drop images{onSubmit && ' · ⌘↵ to send'}</>}</Text>
                {onSubmit && <Button size="compact-sm" onClick={submit} loading={submitting} disabled={!canSubmit || pending > 0}>{submitLabel}</Button>}
            </Group>
        </Box>
    )
}
