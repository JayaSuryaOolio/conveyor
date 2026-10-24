import {useEffect, useRef, useState} from 'react'
import {Box, Button, Code, Group, SegmentedControl, Stack, Switch, Text, TextInput, Title} from '@mantine/core'
import {IconCopy, IconSearch} from '@tabler/icons-react'
import {AppLog} from '../wailsjs/go/ui/App'

const MAX_LINES = 5000
const MAX_PATH = 200
const SOURCES = [
    {value: 'app', label: 'App', file: 'conveyor.log'},
    {value: 'dispatcher', label: 'Dispatcher', file: 'logs/dispatcher.log'},
    {value: 'hooks', label: 'Hooks', file: 'logs/hook.log'},
]

export function Logs() {
    const [lines, setLines] = useState<string[]>([])
    const [q, setQ] = useState('')
    const [follow, setFollow] = useState(true)
    const [source, setSource] = useState('app')
    const [loaded, setLoaded] = useState(false)
    const box = useRef<HTMLDivElement>(null)

    useEffect(() => {
        let next = -1, partial = '', dead = false
        setLines([])
        setLoaded(false)
        const poll = () => AppLog(source, next).then(r => {
            if (dead) return
            setLoaded(true)
            const reset = next >= 0 && r.next < next // log rotated: start over
            next = r.next
            if (!r.text) return
            const parts = ((reset ? '' : partial) + r.text).split('\n')
            partial = parts.pop() ?? ''
            setLines(old => [...(reset ? [] : old), ...parts].slice(-MAX_LINES))
        }, () => {})
        poll()
        const t = setInterval(poll, 2000)
        return () => { dead = true; clearInterval(t) }
    }, [source])

    const file = SOURCES.find(s => s.value === source)!.file
    const needle = q.toLowerCase()
    const shown = needle ? lines.filter(l => l.toLowerCase().includes(needle)) : lines

    useEffect(() => {
        if (follow && box.current) box.current.scrollTop = box.current.scrollHeight
    }, [shown.length, follow, q])

    // Scrolling up turns Follow off; scrolling back to the bottom turns it on.
    const onScroll = () => {
        const b = box.current!
        setFollow(b.scrollHeight - b.scrollTop - b.clientHeight < 8)
    }

    return (
        <Stack h="calc(100vh - 2 * var(--mantine-spacing-md))" gap="xs">
            <Group justify="space-between">
                <Title order={3}>Logs</Title>
                <Group>
                    <SegmentedControl value={source} onChange={setSource} data={SOURCES.map(({value, label}) => ({value, label}))}/>
                    <TextInput placeholder="Filter" leftSection={<IconSearch size={16}/>} value={q} onChange={e => setQ(e.target.value)}/>
                    <Switch label="Follow" checked={follow} onChange={e => setFollow(e.currentTarget.checked)}/>
                    <Button variant="default" leftSection={<IconCopy size={16}/>} onClick={() => navigator.clipboard.writeText(shown.join('\n'))}>Copy</Button>
                </Group>
            </Group>
            <Text size="xs" c="dimmed">~/.conveyor/{file}. From a terminal: <Code>tail -f ~/.conveyor/{file}</Code></Text>
            <Box ref={box} onScroll={onScroll} flex={1} mih={0} p="xs" bd="1px solid var(--mantine-color-default-border)"
                 style={{overflow: 'auto', fontFamily: 'var(--mantine-font-family-monospace)', fontSize: 12, borderRadius: 4}}>
                {loaded && !lines.length && <Text size="sm" c="dimmed">Nothing logged to {file} yet.</Text>}
                {shown.map((l, i) => {
                    const lo = l.toLowerCase()
                    const path = l.includes('PATH=')
                    return (
                        <div key={i} style={{whiteSpace: 'pre-wrap', wordBreak: 'break-all',
                            color: lo.includes('error') || lo.includes('fail') ? 'var(--mantine-color-red-6)' : path ? 'var(--mantine-color-dimmed)' : undefined}}>
                            {path && l.length > MAX_PATH ? l.slice(0, MAX_PATH) + '…' : l}
                        </div>
                    )
                })}
            </Box>
        </Stack>
    )
}
