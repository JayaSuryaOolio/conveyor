// Run: node --experimental-strip-types --test frontend/test/router.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import {fromLocation, toPath} from '../src/router.ts'

const loc = p => { const u = new URL(p, 'http://x'); return {pathname: u.pathname, search: u.search} }
const strip = v => JSON.parse(JSON.stringify(v)) // drop undefined fields

test('every view survives a URL round trip', () => {
    for (const v of [{name: 'board'}, {name: 'board', label: 'epic'}, {name: 'board', repo: 'a/b'}, {name: 'task', key: 'CV-4A6D'},
        {name: 'epic', key: 'CV-4A6'}, {name: 'epic', key: 'CV-4A6', task: 'CV-4A6D'}, {name: 'logs'}])
        assert.deepEqual(strip(fromLocation(loc(toPath(v)))), v, toPath(v))
})

test('deep link shape; root is the board', () => {
    assert.equal(toPath({name: 'epic', key: 'CV-4A6', task: 'CV-4A6D'}), '/epic/CV-4A6/task/CV-4A6D')
    assert.equal(fromLocation(loc('/')).name, 'board')
})
