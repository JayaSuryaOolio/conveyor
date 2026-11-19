import type {Page} from '@playwright/test'

const card = (key: string, title: string, status: string, extra: object = {}) => ({
    key, title, status, repos: ['conveyor'], labels: ['ui'], prUrls: [], priority: 1, updatedAt: new Date().toISOString(),
    lastMsgAt: new Date().toISOString(), lastMsgBy: 'agent', lastSummary: 'summary', ctxPeak: 0, openPR: false, parent: '', ...extra,
})
export const TASKS = [card('CV-1', 'First task', 'todo'), card('CV-2', 'Second task', 'needs_you'), card('CV-3', 'Third task', 'review')]

/** Replaces the Wails bridge: every bound Go method resolves to an empty list unless listed here. */
export async function mockWails(page: Page) {
    await page.addInitScript(tasks => {
        const fixed: Record<string, unknown> = {ListTasks: tasks, ShellPATHError: '', DispatcherState: '', AppVersion: 'test', Home: '/home'}
        const App = new Proxy({}, {get: (_, name: string) => async () => (name in fixed ? fixed[name] : [])})
        const noop = () => {}
        ;(window as any).go = {ui: {App}}
        ;(window as any).runtime = new Proxy({}, {get: (_, name: string) => (name === 'EventsOnMultiple' ? () => noop : noop)})
    }, TASKS)
}
