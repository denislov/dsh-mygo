import assert from 'node:assert/strict'
import { test } from 'node:test'
import { MygoServer } from '../server.mjs'

const handshake = { protocolVersion: 1, client: { name: 'test', version: '0.1.1', platform: 'test', locale: 'en-US' }, capabilities: ['subagent.inspection'] }

function fixture() {
  const entries = [{ id: 'child', mode: 'continuable', label: 'Inspect the repository', createdAt: 1 }]
  const events = Array.from({ length: 125 }, (_, seq) => ({ seq, time: seq, type: 'assistant/message', data: { message: { content: [{ type: 'text', text: `Reply ${seq}` }] } } }))
  let closed = 0
  let observed = 0
  const source = { header: { id: 'child', parentSession: 'parent', origin: 'subagent', isSeeded: true }, inheritedEventCount: 5, cursor: 124, events }
  const ctx = {
    get(name) { return this[name] }, on() { return () => {} },
    subagents: { async listChildren(id) { assert.equal(id, 'parent'); return entries }, async drainDescendants() {} },
    sessionQuery: { async observeSession(id) { observed++; assert.equal(id, 'child'); return { ...source, [Symbol.dispose]() { closed++ } } } },
    agents: { get() { return undefined } },
  }
  const server = new MygoServer(ctx, { notify() {}, createUserMessage: v => v, reasoningEffort: v => v, dshVersion: '0.2.1-rc.2' })
  server.sessions.set('parent', { handle: { agent: { session: { id: 'parent' }, status: 'running' }, async dispose() {} }, calls: new Map() })
  return { server, ctx, entries, source, events, counts: () => ({ closed, observed }) }
}

test('inspection negotiates independently and rejects old compositions', async () => {
  const f = fixture()
  assert.deepEqual((await f.server.handle('initialize', handshake)).capabilities, ['subagent.inspection'])
  const result = await f.server.handle('subagent/list', { parentSessionId: 'parent' })
  assert.equal(result.parentSessionId, 'parent')
  assert.equal(result.children[0].label, 'Inspect the repository')
  assert.equal(result.children[0].status, 'stored')
  assert.deepEqual(f.counts(), { closed: 0, observed: 0 })
  const old = fixture()
  delete old.ctx.sessionQuery
  assert.deepEqual((await old.server.handle('initialize', handshake)).capabilities, [])
  await assert.rejects(old.server.handle('subagent/list', { parentSessionId: 'parent' }), { kind: 'protocol/capability-unavailable' })
})

test('inspection authorizes the owned direct parent before reading child history', async () => {
  const f = fixture()
  await f.server.handle('initialize', handshake)
  for (const parentSessionId of ['foreign', '../parent', null]) {
    await assert.rejects(f.server.handle('subagent/history', { parentSessionId, childSessionId: 'child' }))
  }
  await assert.rejects(f.server.handle('subagent/history', { parentSessionId: 'parent', childSessionId: 'foreign' }), { kind: 'subagent/not-found' })
  assert.equal(f.counts().observed, 0)
  f.source.header.parentSession = 'foreign'
  await assert.rejects(f.server.handle('subagent/history', { parentSessionId: 'parent', childSessionId: 'child' }), { kind: 'subagent/unavailable' })
  assert.deepEqual(f.counts(), { observed: 1, closed: 1 })
})

test('inspection pages the child own suffix without resuming an agent', async () => {
  const f = fixture()
  await f.server.handle('initialize', handshake)
  const params = { parentSessionId: 'parent', childSessionId: 'child' }
  const tail = await f.server.handle('subagent/history', params)
  assert.equal(tail.startSeq, 61)
  assert.equal(tail.endSeq, 125)
  assert.equal(tail.nextSeq, 125)
  assert.equal(tail.inheritedEventCount, 5)
  assert.equal(tail.events.length, 64)
  assert.equal(tail.hasMore, true)
  const older = await f.server.handle('subagent/history', { ...params, beforeSeq: tail.startSeq })
  assert.equal(older.startSeq, 5)
  assert.equal(older.endSeq, 61)
  assert.equal(older.hasMore, false)
  assert.deepEqual(f.counts(), { observed: 2, closed: 2 })
  for (const beforeSeq of [null, -1, 126, '61', 4]) {
    await assert.rejects(f.server.handle('subagent/history', { ...params, beforeSeq }), { kind: 'protocol/invalid-params' })
  }
})

test('inspection removes hidden payloads and reasoning while preserving sequence offsets', async () => {
  const f = fixture()
  f.source.cursor = 9
  f.events[5] = { seq: 5, time: 5, type: 'request/header', data: { token: 'secret-marker' } }
  f.events[6] = { seq: 6, time: 6, type: 'subagent/descriptor', data: { persona: 'secret-marker' } }
  f.events[7] = { seq: 7, time: 7, type: 'assistant/message', data: { stream: 'secret-marker', message: { content: [{ type: 'reasoning', text: 'secret-marker' }, { type: 'text', text: 'Visible result' }] } } }
  await f.server.handle('initialize', handshake)
  const page = await f.server.handle('subagent/history', { parentSessionId: 'parent', childSessionId: 'child' })
  assert.deepEqual(page.events.map(e => e.seq), [5, 6, 7, 8, 9])
  assert.ok(!JSON.stringify(page).includes('secret-marker'))
  assert.ok(JSON.stringify(page).includes('Visible result'))
  assert.equal(page.events[0].type, 'mygo/hidden')
})

test('inspection bounds catalogs and pages, rejects external history and closes failed observations', async () => {
  const f = fixture()
  await f.server.handle('initialize', handshake)
  f.entries[0].mode = 'external'
  assert.equal((await f.server.handle('subagent/list', { parentSessionId: 'parent' })).children[0].status, 'external')
  await assert.rejects(f.server.handle('subagent/history', { parentSessionId: 'parent', childSessionId: 'child' }), { kind: 'subagent/unavailable' })
  f.entries[0].mode = 'continuable'
  f.events[124].data.message.content[0].text = 'x'.repeat(300000)
  await assert.rejects(f.server.handle('subagent/history', { parentSessionId: 'parent', childSessionId: 'child' }), { kind: 'subagent/page-too-large' })
  assert.deepEqual(f.counts(), { observed: 1, closed: 1 })
  f.entries.push(...Array.from({ length: 128 }, (_, i) => ({ id: `extra-${i}`, mode: 'one-shot', createdAt: 1 })))
  await assert.rejects(f.server.handle('subagent/list', { parentSessionId: 'parent' }), { kind: 'subagent/catalog-too-large' })
})

test('inspection pins the parent against release and shutdown aborts outstanding reads', async () => {
  const f = fixture()
  f.ctx.subagents.listChildren = async (_id, signal) => new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new Error('secret-marker')), { once: true }))
  await f.server.handle('initialize', handshake)
  const pending = f.server.handle('subagent/list', { parentSessionId: 'parent' })
  const failure = assert.rejects(pending, e => e.kind === 'subagent/read-failed' && !e.message.includes('secret-marker'))
  assert.equal(f.server.sessions.get('parent').catalogReaders, 1)
  await f.server.shutdown()
  await failure
  assert.equal(f.server.sessions.size, 0)
})

test('inspection never trusts a dependency exception kind or message', async () => {
  const f = fixture()
  f.ctx.sessionQuery.observeSession = async () => { throw Object.assign(new Error('secret-marker'), { kind: 'subagent/unavailable', code: -1 }) }
  await f.server.handle('initialize', handshake)
  await assert.rejects(f.server.handle('subagent/history', { parentSessionId: 'parent', childSessionId: 'child' }), error => {
    assert.equal(error.kind, 'subagent/read-failed')
    assert.equal(error.message, 'Could not read subagent information. Refresh to retry')
    return true
  })
  assert.equal(f.server.sessions.get('parent').catalogReaders, 0)
})
