import {expect, Page, test} from '@playwright/test'
import {mockWails} from './mock'

test.beforeEach(async ({page}) => {
    await mockWails(page)
    await page.goto('/')
    await page.getByText('First task').waitFor()
})

/** Describes the focused element and whether it shows a visible indicator (outline or box-shadow). */
const focused = (page: Page) => page.evaluate(() => {
    const el = document.activeElement as HTMLElement
    const shows = (e: Element) => { const s = getComputedStyle(e); return (s.outlineStyle !== 'none' && parseFloat(s.outlineWidth) > 0) || s.boxShadow !== 'none' }
    const ring = shows(el) || (!!el.closest('.find-box') && shows(el.closest('.find-box')!)) // the unstyled search input rings its bar
    return {tag: el.tagName, label: el.getAttribute('aria-label') || el.textContent?.trim().slice(0, 40) || '', ring, inDialog: !!el.closest('[role=dialog]')}
})

test('every Tab stop on the board shows a focus ring', async ({page}) => {
    await page.locator('body').click({position: {x: 1, y: 1}})
    const seen: string[] = []
    for (let i = 0; i < 40; i++) {
        await page.keyboard.press('Tab')
        const f = await focused(page)
        if (f.tag === 'BODY') break
        expect(f.ring, `no focus ring on ${f.tag} "${f.label}"`).toBe(true)
        seen.push(f.label)
    }
    expect(seen.length).toBeGreaterThan(10)
})

test('board cards are reachable by Tab in column order; Enter and Space open the task', async ({page}) => {
    await page.locator('body').click({position: {x: 1, y: 1}})
    const order: string[] = []
    for (let i = 0; i < 60 && order.length < 3; i++) {
        await page.keyboard.press('Tab')
        const key = await page.evaluate(() => { const el = document.activeElement as HTMLElement; return el.getAttribute('role') === 'button' ? el.textContent!.match(/^CV-\d+/)?.[0] ?? '' : '' })
        if (key) order.push(key)
    }
    expect(order).toEqual(['CV-1', 'CV-2', 'CV-3'])
    await page.getByRole('button', {name: /^CV-2/}).focus()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL(/#\/task\/CV-2/)
    await page.goBack()
    await page.getByText('Third task').waitFor()
    await page.getByRole('button', {name: /^CV-3/}).focus()
    await page.keyboard.press('Space')
    await expect(page).toHaveURL(/#\/task\/CV-3/)
})

test('New task modal takes focus, traps Tab, and returns focus on Escape', async ({page}) => {
    const opener = page.getByRole('button', {name: /New task/})
    await opener.focus()
    await page.keyboard.press('Enter')
    const dlg = page.getByRole('dialog', {name: 'New task'})
    await expect(dlg).toBeVisible()
    for (let i = 0; i < 15; i++) {
        expect((await focused(page)).inDialog, `focus left the modal after ${i} Tabs`).toBe(true)
        await page.keyboard.press('Tab')
    }
    await page.keyboard.press('Escape')
    await expect(dlg).toBeHidden()
    await expect(opener).toBeFocused()
})

test('Focus popup (from the palette) keeps focus inside, incl. nested preview, and restores it', async ({page}) => {
    await page.getByRole('button', {name: 'Board', exact: true}).focus()
    await page.keyboard.press('Control+k')
    await page.getByPlaceholder(/Search tasks/).fill('focus')
    await page.keyboard.press('Enter')
    const dlg = page.getByRole('dialog', {name: 'Focus'})
    await expect(dlg).toBeVisible()
    await page.waitForTimeout(300) // let the palette's own close/return-focus settle
    expect((await focused(page)).inDialog, 'focus is not in the Focus popup').toBe(true)
    for (let i = 0; i < 12; i++) {
        await page.keyboard.press('Tab')
        expect((await focused(page)).inDialog, `focus left the popup after ${i + 1} Tabs`).toBe(true)
    }
    await page.keyboard.press('Escape')
    await expect(dlg).toBeHidden()
    expect((await focused(page)).tag, 'focus dropped to <body> after the popup closed').not.toBe('BODY')
})
