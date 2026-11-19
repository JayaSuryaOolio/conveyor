/**
 * The only module (with theme setup in main.tsx/App.tsx) that knows these widgets come from Mantine.
 * Call sites use the library-neutral props below, so swapping the component library means rewriting this file, not every screen.
 * Rule: never pass a Mantine prop type or Mantine-only prop (withBorder, radius, p, w, shadow, withCloseButton, ...) through a wrapper; add a neutral prop and map it here.
 * Migrating other widgets (Button, Text, Stack, ...) follows the same pattern, one wrapper at a time.
 */
import {Modal as MModal, Paper} from '@mantine/core'
import type {HTMLAttributes, ReactNode} from 'react'
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
