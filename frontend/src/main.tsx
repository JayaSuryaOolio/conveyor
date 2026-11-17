import React from 'react'
import {createRoot} from 'react-dom/client'
import {Anchor, MultiSelect, Select, TagsInput, createTheme} from '@mantine/core'
import '@mantine/core/styles.css'
import {HashRouter} from 'react-router'
import './style.css'
import App from './App'
import {ThemeRoot} from './themes'
import {Ready} from '../wailsjs/go/ui/App'

// Every dropdown can be typed into to filter it. autoComplete off: the browser's own suggestion popover swallows the arrow keys meant for the options.
// Plain TextInputs keep it. Refactor idea: a shared `selectDefaults` const if a 4th select-like component is added.
const theme = createTheme({
    components: {
        Select: Select.extend({defaultProps: {searchable: true, autoComplete: 'off'}}),
        MultiSelect: MultiSelect.extend({defaultProps: {searchable: true, autoComplete: 'off'}}),
        TagsInput: TagsInput.extend({defaultProps: {autoComplete: 'off'}}),
        // Links: muted gray (not blue) so they read as clickable without grabbing attention; underline is the affordance.
        Anchor: Anchor.extend({defaultProps: {c: 'light-dark(var(--mantine-color-gray-7), var(--mantine-color-gray-4))', underline: 'always'}}),
    },
})

const container = document.getElementById('root')

const root = createRoot(container!)

// Wails runs Startup (opens the DB) concurrently with the page load; a binding called before it finishes never answers,
// so wait for it before mounting anything that loads data.
Ready().then(() => root.render(
    <React.StrictMode>
        <ThemeRoot base={theme}>
            <HashRouter>
                <App/>
            </HashRouter>
        </ThemeRoot>
    </React.StrictMode>
))
