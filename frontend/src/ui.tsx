/** Shared wrappers over Mantine: focus, ring and a11y defaults live here, call sites import from './ui' instead of '@mantine/core' for these. */
import {Modal as MModal, ModalProps, Paper, PaperProps, Card, CardProps} from '@mantine/core'
import {ElementType, ReactNode} from 'react'
import {clickable} from './util'

/** Modal with an accessible close button; focus trap and return-focus are Mantine's defaults (the Modal must stay mounted with `opened` toggling, so the opener is recorded). */
export const Modal = (p: ModalProps) => <MModal closeButtonProps={{'aria-label': 'Close'}} {...p}/>

type Click = {onActivate: () => void}
/** Keyboard-operable card (Enter/Space activate, focus ring from style.css). */
export const ClickableCard = ({onActivate, style, ...p}: CardProps & Click & {children?: ReactNode}) =>
    <Card style={{cursor: 'pointer', ...style}} {...clickable(onActivate)} {...(p as object)}/>
/** Same for Paper rows. `component` is not forwarded so role=button stays on a div. */
export const ClickablePaper = ({onActivate, style, ...p}: PaperProps & Click & {children?: ReactNode; [k: string]: unknown}) =>
    <Paper style={{cursor: 'pointer', ...style}} {...clickable(onActivate)} {...(p as object)}/>
