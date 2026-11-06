// Run: node --experimental-strip-types --test frontend/test/epicLayout.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import {columns, layout, CARD} from '../src/epicLayout.ts'

test('column = longest blocks path inside the group; cross-group edges ignored', () => {
    const c = columns(['A', 'B', 'C', 'D'], [
        {from: 'A', to: 'B', kind: 'blocks'}, {from: 'B', to: 'C', kind: 'blocks'}, {from: 'A', to: 'C', kind: 'blocks'},
        {from: 'X', to: 'D', kind: 'blocks'}, {from: 'A', to: 'D', kind: 'relates'},
    ])
    assert.deepEqual([...c], [['A', 0], ['B', 1], ['C', 2], ['D', 0]])
})

test('children sit inside their parent group, rows ordered by key', () => {
    const p = layout([{key: 'E', parent: ''}, {key: 'E2', parent: 'E'}, {key: 'E1', parent: 'E'}], [])
    const by = Object.fromEntries(p.map(n => [n.key, n]))
    assert.equal(by.E.group, true)
    assert.equal(by.E1.parent, 'E')
    assert.ok(by.E1.y < by.E2.y)
    assert.ok(by.E.h >= by.E2.y + CARD.h)
})

test('measured heights drive row spacing and group height', () => {
    const p = layout([{key: 'E', parent: ''}, {key: 'E1', parent: 'E'}, {key: 'E2', parent: 'E'}], [], new Map([['E1', 400]]))
    const by = Object.fromEntries(p.map(n => [n.key, n]))
    assert.equal(by.E1.h, 400)
    assert.equal(by.E2.y - by.E1.y, 400 + CARD.gap)
    assert.ok(by.E.h >= by.E2.y + CARD.h)
})

test('a lone task is one node', () => {
    assert.equal(layout([{key: 'T', parent: ''}], []).length, 1)
})
