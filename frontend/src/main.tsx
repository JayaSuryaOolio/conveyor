import React from 'react'
import {createRoot} from 'react-dom/client'
import {Anchor, MantineProvider, MultiSelect, Select, createTheme} from '@mantine/core'
import '@mantine/core/styles.css'
import './style.css'
import App from './App'

// Every dropdown can be typed into to filter it.
const theme = createTheme({
    components: {
        Select: Select.extend({defaultProps: {searchable: true}}),
        MultiSelect: MultiSelect.extend({defaultProps: {searchable: true}}),
        // Links: muted gray (not blue) so they read as clickable without grabbing attention; underline is the affordance.
        Anchor: Anchor.extend({defaultProps: {c: 'light-dark(var(--mantine-color-gray-7), var(--mantine-color-gray-4))', underline: 'always'}}),
    },
})

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <MantineProvider theme={theme} defaultColorScheme="dark">
            <App/>
        </MantineProvider>
    </React.StrictMode>
)
