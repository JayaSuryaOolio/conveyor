import {ReactNode, createContext, useContext, useState} from 'react'
import {Box, Combobox, InputBase, MantineColorsTuple, MantineProvider, MantineThemeOverride, Text, mergeThemeOverrides, useCombobox} from '@mantine/core'
import {Group, Stack} from './ui'

type Theme = {id: string; label: string; scheme: 'light' | 'dark'; theme: MantineThemeOverride}

// Dark themes recolor Mantine's `dark` tuple: [0] text, [4] borders, [6] inputs/papers, [7] body.
// Light themes recolor `white` (body, papers, inputs), `black` (text) and `gray` (borders, hovers).
const tuple = (s: string) => s.split(' ') as unknown as MantineColorsTuple
export const THEMES: Theme[] = [
    {id: 'dark', label: 'Dark', scheme: 'dark', theme: {}},
    {id: 'light', label: 'Light', scheme: 'light', theme: {}},
    {id: 'midnight', label: 'Midnight blue', scheme: 'dark', theme: {primaryColor: 'indigo', colors: {dark: tuple('#c9d1e3 #aab4cc #7d89a8 #5c6888 #3a4562 #2c3653 #222b45 #1a2238 #141b2e #0d1322')}}},
    {id: 'nord', label: 'Nord', scheme: 'dark', theme: {primaryColor: 'cyan', colors: {dark: tuple('#eceff4 #d8dee9 #a3abba #7b8598 #4c566a #434c5e #3b4252 #2e3440 #292e39 #22262f')}}},
    {id: 'mocha', label: 'Mocha', scheme: 'dark', theme: {primaryColor: 'violet', colors: {dark: tuple('#cdd6f4 #bac2de #9399b2 #7f849c #585b70 #45475a #313244 #1e1e2e #181825 #11111b')}}},
    {id: 'evergreen', label: 'Evergreen', scheme: 'dark', theme: {primaryColor: 'teal', colors: {dark: tuple('#d3dfd8 #b5c5bc #84988d #647a6e #3f5248 #33453b #283930 #1e2c25 #18241e #101a15')}}},
    // Same recipe as Evergreen (muted neutrals tinted toward the accent), tinted warm brown with an orange accent.
    {id: 'ember', label: 'Ember', scheme: 'dark', theme: {primaryColor: 'orange', colors: {dark: tuple('#e3d8d0 #c8b9ad #9a8a7d #7b6c60 #53443a #45382f #382d25 #2b221c #221a15 #19120e')}}},
    {id: 'sky', label: 'Sky', scheme: 'light', theme: {primaryColor: 'blue', white: '#f6f9fd', black: '#1c2b3a', colors: {gray: tuple('#eef3f9 #e2e9f2 #d0dae7 #b8c5d6 #93a3b8 #73849b #56677d #3e4d61 #2a3747 #18222f')}}},
    {id: 'paper', label: 'Paper', scheme: 'light', theme: {primaryColor: 'teal', white: '#fbf8f1', black: '#3b3a36', colors: {gray: tuple('#f5f1e8 #ede7da #e2dac9 #d3c9b4 #b8ad97 #9a8f7a #7c7261 #5f574a #433d34 #2b2721')}}},
]
const byId = (id: string | null) => THEMES.find(t => t.id === id) ?? THEMES[0]

const KEY = 'ui:theme'
const Ctx = createContext({theme: THEMES[0], set: (_id: string) => {}, preview: (_id: string | null) => {}})

/** MantineProvider with the picked theme (kept in localStorage, per machine) or, while the picker is open, the highlighted one. */
export function ThemeRoot({base, children}: {base: MantineThemeOverride; children: ReactNode}) {
    const [id, setId] = useState(() => { try { return localStorage.getItem(KEY) } catch { return null } })
    const [previewId, preview] = useState<string | null>(null)
    const set = (v: string) => {
        setId(v); preview(null)
        try { localStorage.setItem(KEY, v) } catch { /* private mode */ }
    }
    const shown = byId(previewId ?? id)
    return (
        <Ctx.Provider value={{theme: byId(id), set, preview}}>
            <MantineProvider theme={mergeThemeOverrides(base, shown.theme)} forceColorScheme={shown.scheme}>{children}</MantineProvider>
        </Ctx.Provider>
    )
}

/** Primary, body and text colors of a theme, as a small preview. */
function Swatch({t}: {t: Theme}) {
    const c = (name: string, i: number) => (t.theme.colors?.[name] as string[] | undefined)?.[i]
    const body = t.scheme === 'dark' ? c('dark', 7) ?? '#242424' : t.theme.white ?? '#fff'
    const text = t.scheme === 'dark' ? c('dark', 0) ?? '#c9c9c9' : t.theme.black ?? '#000'
    return (
        <Group gap={0} style={{border: '1px solid var(--mantine-color-default-border)', borderRadius: 4, overflow: 'hidden'}}>
            {[body, text, `var(--mantine-color-${t.theme.primaryColor ?? 'blue'}-filled)`].map((bg, i) => <Box key={i} w={12} h={16} bg={bg}/>)}
        </Group>
    )
}

/** Theme dropdown; hovering or arrowing onto an option previews it (hover also moves the keyboard cursor), closing without picking restores the current one. */
export function ThemePicker() {
    const {theme, set, preview} = useContext(Ctx)
    const combobox = useCombobox({onDropdownClose: () => { combobox.resetSelectedOption(); preview(null) }})
    // The keyboard-selected index lives in a ref, so read it after Mantine's own arrow-key handler has moved it.
    const previewSelected = () => setTimeout(() => { const i = combobox.getSelectedOptionIndex(); if (i >= 0) preview(THEMES[i].id) })
    return (
        <Combobox store={combobox} onOptionSubmit={v => { set(v); combobox.closeDropdown() }}>
            <Combobox.Target>
                <InputBase component="button" type="button" pointer label="Theme" w={260} rightSection={<Combobox.Chevron/>} rightSectionPointerEvents="none"
                           onClick={() => combobox.toggleDropdown()} onKeyDown={previewSelected}>
                    <Group gap="xs"><Swatch t={theme}/><Text size="sm">{theme.label}</Text></Group>
                </InputBase>
            </Combobox.Target>
            <Combobox.Dropdown>
                <Combobox.Options>
                    {THEMES.map((t, i) => (
                        <Combobox.Option key={t.id} value={t.id} active={t.id === theme.id} onMouseEnter={() => { combobox.selectOption(i); preview(t.id) }}>
                            <Group gap="xs"><Swatch t={t}/><Text size="sm">{t.label}</Text></Group>
                        </Combobox.Option>
                    ))}
                </Combobox.Options>
            </Combobox.Dropdown>
        </Combobox>
    )
}
