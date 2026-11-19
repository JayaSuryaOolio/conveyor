/**
 * The only module (with theme setup in main.tsx/App.tsx) that knows these widgets come from Mantine.
 * Call sites use the library-neutral props below, so swapping the component library means rewriting this file, not every screen.
 * Rule: never pass a Mantine prop type or Mantine-only prop (withBorder, radius, p, w, shadow, withCloseButton, ...) through a wrapper; add a neutral prop and map it here.
 * Migrating other widgets (Button, Text, Stack, ...) follows the same pattern, one wrapper at a time.
 */
import {Button as MButton, Code, Group as MGroup, Modal as MModal, Paper, Stack as MStack, Text as MText} from '@mantine/core'
import type {CSSProperties, ElementType, HTMLAttributes, ImgHTMLAttributes, MouseEvent, ReactNode} from 'react'
import {clickable} from './util'

type Size = 'sm' | 'md' | 'lg' | 'auto' | (string & {}) // 'sm'|'md'|'lg' are our tokens; anything else is a CSS width

export type ModalProps = {
    opened: boolean
    onClose: () => void
    title?: ReactNode
    size?: Size
    compact?: boolean // tighter body padding
    centered?: boolean
    closeButton?: boolean // default true
    keepMounted?: boolean // keep the body in the DOM while closed
    onExitTransitionEnd?: () => void // after the close animation
    children?: ReactNode
}

/** Dialog. Keep it mounted with `opened` toggled (not conditionally rendered) so focus trap and return-focus see the opener. */
export const Modal = ({closeButton = true, compact, ...p}: ModalProps) =>
    <MModal withCloseButton={closeButton} padding={compact ? 'sm' : undefined} closeButtonProps={{'aria-label': 'Close'}} {...p}/>

export type ClickableCardProps = Omit<HTMLAttributes<HTMLDivElement>, 'onClick'> & {
    onActivate: () => void // click, Enter or Space
    bordered?: boolean
    raised?: boolean // drop shadow
    padding?: 'xs' | 'sm'
    width?: number | string
}

/** Keyboard-operable card or row (role=button, focus ring from style.css). Buttons inside keep their own keys. */
export const ClickableCard = ({onActivate, bordered, raised, padding = 'sm', width, style, ...p}: ClickableCardProps) =>
    <Paper withBorder={bordered} shadow={raised ? 'sm' : undefined} radius="md" p={padding} w={width} style={{cursor: 'pointer', ...style}} {...clickable(onActivate)} {...p}/>

type ClickableProps = Omit<HTMLAttributes<HTMLElement>, 'onClick'> & ImgHTMLAttributes<HTMLImageElement> & {
    as?: 'div' | 'img'
    onActivate?: () => void // click, Enter or Space; without it the element is inert (conditional rows)
    style?: CSSProperties
}

/** Any plain element made keyboard-operable, for spots that are not a card (rows, headers, images). Style it with `style`/`className`. */
export const Clickable = ({as: C = 'div', onActivate, style, ...p}: ClickableProps) =>
    <C style={onActivate ? {cursor: 'pointer', ...style} : style} {...(onActivate ? clickable(onActivate) : {})} {...p}/>

/** Monospace chip that activates on click, Enter or Space (copy-to-clipboard values). */
export const ClickableCode = ({onActivate, block, style, ...p}: HTMLAttributes<HTMLElement> & {onActivate: () => void; block?: boolean}) =>
    <Code style={{cursor: 'pointer', display: block ? 'block' : undefined, ...style}} {...clickable(onActivate)} {...p}/>

export type ButtonSize = 'md' | 'sm' | 'xs'
export type ButtonProps = {
    variant?: 'primary' | 'secondary' | 'ghost' | 'soft' // default primary
    tone?: 'danger' | 'success' | 'muted'
    size?: ButtonSize
    compact?: boolean
    icon?: ReactNode // leading
    trailing?: ReactNode
    loading?: boolean
    disabled?: boolean
    type?: 'button' | 'submit'
    id?: string
    title?: string
    style?: CSSProperties
    onClick?: (e: MouseEvent<HTMLButtonElement>) => void
    children?: ReactNode
    'data-autofocus'?: boolean
    'aria-pressed'?: boolean
}
const BTN_VARIANT = {primary: 'filled', secondary: 'default', ghost: 'subtle', soft: 'light'} as const
const BTN_TONE = {danger: 'red', success: 'green', muted: 'gray'} as const

export const Button = ({variant = 'primary', tone, size, compact, icon, trailing, ...p}: ButtonProps) =>
    <MButton variant={BTN_VARIANT[variant]} color={tone && BTN_TONE[tone]} size={compact ? `compact-${size ?? 'md'}` : size} leftSection={icon} rightSection={trailing} {...p}/>

export type Gap = number | 'xs' | 'sm' | 'md' | 'lg' | 'xl' // number = px
type Flow = {
    gap?: Gap
    align?: 'start' | 'center' | 'end' | 'stretch'
    justify?: 'start' | 'center' | 'end' | 'between'
    as?: ElementType
    id?: string
    title?: string
    role?: string
    className?: string
    style?: CSSProperties
    children?: ReactNode
    onClick?: (e: MouseEvent<HTMLElement>) => void
    onMouseEnter?: (e: MouseEvent<HTMLElement>) => void
    onMouseDown?: (e: MouseEvent<HTMLElement>) => void
    'aria-label'?: string
}
const FLEX = {start: 'flex-start', end: 'flex-end', between: 'space-between', center: 'center', stretch: 'stretch'} as const
const flow = ({align, justify, as, ...p}: Flow) => ({align: align && FLEX[align], justify: justify && FLEX[justify], component: as, ...p})

/** Vertical flex column. Spacing/size beyond these props goes in `style`. */
export const Stack = (p: Flow) => <MStack {...flow(p)}/>
/** Horizontal flex row; wraps by default, `wrap={false}` keeps one line, `grow` stretches children equally. */
export const Group = ({wrap = true, grow, ...p}: Flow & {wrap?: boolean; grow?: boolean}) =>
    <MGroup wrap={wrap ? 'wrap' : 'nowrap'} grow={grow} {...flow(p)}/>

export type TextTone = 'muted' | 'danger' | 'warning' | 'accent' | 'info' | 'live' | 'neutral'
export type TextProps = {
    size?: 'md' | 'sm' | 'xs'
    tone?: TextTone
    weight?: 400 | 500 | 600 | 700
    mono?: boolean
    upper?: boolean
    span?: boolean
    clamp?: number
    truncate?: boolean | 'start' | 'end'
    as?: 'button'
    type?: 'button' | 'submit'
    title?: string
    style?: CSSProperties
    onClick?: (e: MouseEvent<HTMLElement>) => void
    children?: ReactNode
}
const TEXT_TONE = {muted: 'dimmed', danger: 'red', warning: 'orange', accent: 'violet', info: 'blue', live: 'cyan', neutral: 'gray'} as const

export const Text = ({tone, weight, mono, upper, clamp, as, ...p}: TextProps) =>
    <MText c={tone && TEXT_TONE[tone]} fw={weight} ff={mono ? 'monospace' : undefined} tt={upper ? 'uppercase' : undefined} lineClamp={clamp} component={as as 'button' | undefined} {...p}/>
