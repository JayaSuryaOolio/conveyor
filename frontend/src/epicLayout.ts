/** Layering for the epic view (spec §11): ours, no dagre. Pure so it can be tested without React. */
export type LNode = {key: string; parent: string}
export type LLink = {from: string; to: string; kind: string}
export type Placed = {key: string; x: number; y: number; w: number; h: number; group: boolean; parent?: string}

// CARD.h is only the first-paint estimate; layout() takes measured heights (React Flow node.measured) and re-runs once they arrive.
export const CARD = {w: 260, h: 112, gap: 20, pad: 16, head: 44}

/** Column of each key within its group: the longest `blocks` path from a blocker in the same group. Cross-group edges never count. */
export function columns(keys: string[], links: LLink[]): Map<string, number> {
    const inGroup = new Set(keys)
    const blockers = new Map<string, string[]>()
    for (const l of links) if (l.kind === 'blocks' && inGroup.has(l.from) && inGroup.has(l.to)) blockers.set(l.to, [...(blockers.get(l.to) ?? []), l.from])
    const col = new Map<string, number>()
    const walk = (k: string, seen: Set<string>): number => {
        if (col.has(k)) return col.get(k)!
        if (seen.has(k)) return 0 // a cycle cannot exist (link refuses it); stay finite anyway
        seen.add(k)
        const c = Math.max(-1, ...(blockers.get(k) ?? []).map(b => walk(b, seen))) + 1
        col.set(k, c)
        return c
    }
    keys.forEach(k => walk(k, new Set()))
    return col
}

/** Positions every node: children sit inside their parent's group (positions relative to it), roots start at 0,0. `heights` are measured leaf-card heights by key; missing keys fall back to CARD.h. */
export function layout(nodes: LNode[], links: LLink[], heights: ReadonlyMap<string, number> = new Map()): Placed[] {
    const kids = new Map<string, string[]>()
    const known = new Set(nodes.map(n => n.key))
    for (const n of nodes) {
        const p = known.has(n.parent) ? n.parent : ''
        kids.set(p, [...(kids.get(p) ?? []), n.key])
    }
    const out: Placed[] = []
    // Lays out the children of `parent` and returns the box they need.
    const place = (parent: string, ox: number, oy: number): {w: number; h: number} => {
        const keys = (kids.get(parent) ?? []).sort()
        const col = columns(keys, links)
        const size = new Map<string, {w: number; h: number}>()
        const sub = (k: string) => { // measure a child (a group needs its own children's box first)
            if (!size.has(k)) {
                const have = out.length
                const inner = kids.has(k) ? place(k, CARD.pad, CARD.head) : undefined
                out.length = have // measuring only; positions are set below
                size.set(k, inner ? {w: inner.w + 2 * CARD.pad, h: inner.h + CARD.head + CARD.pad} : {w: CARD.w, h: heights.get(k) ?? CARD.h})
            }
            return size.get(k)!
        }
        const nCols = Math.max(0, ...keys.map(k => col.get(k)! + 1))
        const colW = Array.from({length: nCols}, (_, c) => Math.max(...keys.filter(k => col.get(k) === c).map(k => sub(k).w)))
        const rowY = new Array<number>(nCols).fill(0)
        let h = 0
        for (const k of keys) {
            const c = col.get(k)!, s = sub(k)
            const x = ox + colW.slice(0, c).reduce((a, w) => a + w + CARD.gap, 0), y = oy + rowY[c]
            out.push({key: k, x, y, w: s.w, h: s.h, group: kids.has(k), parent: parent || undefined})
            rowY[c] += s.h + CARD.gap
            h = Math.max(h, rowY[c] - CARD.gap)
            if (kids.has(k)) place(k, CARD.pad, CARD.head)
        }
        return {w: colW.reduce((a, w) => a + w + CARD.gap, -CARD.gap), h}
    }
    // A group's own children are placed relative to it, so the root level starts at the origin.
    const rootKeys = kids.get('') ?? []
    if (rootKeys.length === 0) return out
    place('', 0, 0)
    return out
}

/** Every key linked to `key` through any chain of links, upstream and downstream (the hover trace), including `key`. */
export function trace(key: string, links: LLink[]): Set<string> {
    const seen = new Set([key])
    // Walk one direction at a time so a sibling (shares a blocker) isn't pulled in through the blocker.
    for (const [from, to] of [['from', 'to'], ['to', 'from']] as const) {
        const todo = [key], dir = new Set([key])
        while (todo.length) {
            const k = todo.pop()!
            for (const l of links) if (l[from] === k && !dir.has(l[to])) { dir.add(l[to]); seen.add(l[to]); todo.push(l[to]) }
        }
    }
    return seen
}
