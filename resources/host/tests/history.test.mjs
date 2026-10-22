import assert from 'node:assert/strict'
import { test } from 'node:test'
import { MygoServer } from '../server.mjs'

const handshake = {
  protocolVersion: 1,
  client: { name: 'dsh-mygo', version: '0.1.0', platform: 'test', locale: 'zh-CN' },
  capabilities: ['session.history'],
}

function runtime() {
  const stored = new Map()
  const reads = []
  const handles = []
  const header = { id: 'stored-1', cwd: process.cwd(), createdAt: 100, version: 1, isSeeded: false }
  stored.set(header.id, { header, events: Array.from({ length: 5 }, (_, seq) => ({ seq, type: 'test/event', time: seq, data: {} })) })
  const ctx = {
    get(name) { return this[name] },
    on() { return () => {} },
    llm: { async resolveCallConfig() {} },
    sessionPersistence: {
      async list() { return [...stored.values()].map(({ header, formatStatus }) => ({ header, formatStatus })) },
      async stat(id) { return stored.get(id) },
      async flush() {},
      async open(id, access) {
        assert.equal(access, 'read')
        const handle = {
          closed: false,
          async read(offset, length) { reads.push([offset, length]); return { events: stored.get(id).events.slice(offset, offset + length) } },
          async close() { handle.closed = true },
        }
        handles.push(handle)
        return handle
      },
    },
    agents: {
      async resume({ resumeSessionId, agentOptions }) {
        const source = stored.get(resumeSessionId)
        const handle = {
          agent: { session: { id: resumeSessionId, header: source.header, seq: source.events.length }, status: 'idle', options: agentOptions },
          async dispose() { handle.disposed = true },
        }
        handles.push(handle)
        return handle
      },
    },
  }
  const server = new MygoServer(ctx, { notify() {}, createUserMessage: (value) => value, reasoningEffort: (value) => value, dshVersion: '0.2.1-rc.2' })
  return { server, ctx, stored, reads, handles }
}

const resume = { sessionId: 'stored-1', provider: 'test', model: 'echo' }

test('history requires negotiated persistence and an owned resumed session', async () => {
  const { server } = runtime()
  await server.handle('initialize', { ...handshake, capabilities: [] })
  for (const method of ['session/list', 'session/resume', 'session/history']) {
    await assert.rejects(server.handle(method, resume), { kind: 'protocol/capability-unavailable' })
  }
  await server.shutdown()
  const next = runtime()
  assert.deepEqual((await next.server.handle('initialize', handshake)).capabilities, ['session.history'])
  await assert.rejects(next.server.handle('session/history', resume), { kind: 'session/not-found' })
  const result = await next.server.handle('session/resume', resume)
  assert.equal(result.session.id, resume.sessionId)
  assert.equal(result.session.nextSeq, 5)
  assert.equal(result.session.provider, 'test')
  assert.equal((await next.server.handle('session/resume', resume)).session.nextSeq, 5)
  assert.equal(next.handles.length, 1)
  await next.server.shutdown()
  assert.equal(next.handles[0].disposed, true)
})

test('history pages hold a fixed prefix and always close read handles', async () => {
  const { server, stored, handles, reads } = runtime()
  await server.handle('initialize', handshake)
  await server.handle('session/resume', resume)
  const first = await server.handle('session/history', { sessionId: resume.sessionId, limit: 2 })
  assert.equal(first.hasMore, true)
  assert.equal(first.nextSeq, 5)
  assert.deepEqual(first.events.map((event) => event.seq), [0, 1])
  stored.get(resume.sessionId).events.push({ seq: 5, type: 'test/event', data: {} })
  handles[0].agent.session.seq = 6
  const second = await server.handle('session/history', { sessionId: resume.sessionId, cursor: first.nextCursor, limit: 2 })
  const third = await server.handle('session/history', { sessionId: resume.sessionId, cursor: second.nextCursor, limit: 2 })
  assert.deepEqual(second.events.map((event) => event.seq), [2, 3])
  assert.deepEqual(third.events.map((event) => event.seq), [4])
  assert.equal(third.nextSeq, 5)
  assert.equal(third.hasMore, false)
  assert.equal(third.nextCursor, undefined)
  assert.ok(handles.slice(1).every((handle) => handle.closed))
  assert.ok(reads.every(([, length]) => length <= 2))
  await server.shutdown()
})

test('list pagination is an isolated allowlisted snapshot', async () => {
  const { server, stored } = runtime()
  stored.set('stored-2', { header: { id: 'stored-2', cwd: process.cwd(), createdAt: 200, secret: 'private-token' }, events: [] })
  stored.set('child', { header: { id: 'child', createdAt: 300, origin: 'subagent' }, events: [] })
  stored.set('migration', { header: { id: 'migration', createdAt: 400 }, events: [], formatStatus: 'migration-required' })
  await server.handle('initialize', handshake)
  const first = await server.handle('session/list', { limit: 1 })
  assert.deepEqual(first.sessions.map((session) => session.id), ['stored-2'])
  assert.ok(!JSON.stringify(first).includes('private-token'))
  stored.delete('stored-1')
  stored.set('new', { header: { id: 'new', createdAt: 400 }, events: [] })
  const last = await server.handle('session/list', { cursor: first.nextCursor, limit: 1 })
  assert.deepEqual(last.sessions.map((session) => session.id), ['stored-1'])
  assert.equal(last.nextCursor, undefined)
  await assert.rejects(server.handle('session/history', { sessionId: resume.sessionId, cursor: first.nextCursor }), { kind: 'session/not-found' })
  await server.shutdown()
})

test('invalid ids, cursors, limits, direction and unsupported sessions fail closed', async () => {
  const { server, stored } = runtime()
  await server.handle('initialize', handshake)
  for (const sessionId of ['../secret', '', 'a'.repeat(129), 'stored-1\n']) {
    await assert.rejects(server.handle('session/resume', { ...resume, sessionId }), { code: -32602 })
  }
  for (const limit of [null, 0, -1, 257, 1.5, '2']) {
    await assert.rejects(server.handle('session/list', { limit }), { code: -32602 })
  }
  await assert.rejects(server.handle('session/list', { cursor: 'forged' }), { kind: 'session/invalid-cursor' })
  stored.set('child', { header: { id: 'child', cwd: process.cwd(), createdAt: 1, origin: 'subagent' }, events: [] })
  await assert.rejects(server.handle('session/resume', { ...resume, sessionId: 'child' }), { kind: 'session/unavailable' })
  stored.set('migration', { header: { id: 'migration', cwd: process.cwd(), createdAt: 1 }, events: [], formatStatus: 'migration-required' })
  stored.set('no-cwd', { header: { id: 'no-cwd', createdAt: 1 }, events: [] })
  for (const sessionId of ['unknown', 'migration', 'no-cwd']) {
    await assert.rejects(server.handle('session/resume', { ...resume, sessionId }), { kind: 'session/unavailable' })
  }
  for (const route of [{ provider: null }, { model: '' }]) {
    await assert.rejects(server.handle('session/resume', { ...resume, ...route }), { code: -32602 })
  }
  await server.handle('session/resume', resume)
  await assert.rejects(server.handle('session/history', { sessionId: resume.sessionId, direction: 'backward' }), { code: -32602 })
  await assert.rejects(server.handle('session/history', { sessionId: resume.sessionId, cursor: 'forged' }), { kind: 'session/invalid-cursor' })
  await server.shutdown()
})

test('persistence failures are redacted and history handles close on failure', async () => {
  const { server, ctx, handles } = runtime()
  await server.handle('initialize', handshake)
  await server.handle('session/resume', resume)
  ctx.sessionPersistence.open = async () => {
    const handle = { async read() { throw new Error('private-token') }, async close() { handle.closed = true } }
    handles.push(handle)
    return handle
  }
  await assert.rejects(server.handle('session/history', { sessionId: resume.sessionId }), (error) => error.kind === 'session/history-failed' && !error.message.includes('private-token'))
  assert.equal(handles.at(-1).closed, true)
  ctx.sessionPersistence.list = async () => { throw new Error('private-token') }
  await assert.rejects(server.handle('session/list', {}), (error) => error.kind === 'session/list-failed' && !error.message.includes('private-token'))
  await server.shutdown()
})

test('busy sessions refuse history and paging rejects cross-method and cross-session cursors', async () => {
  const { server, stored, handles, reads } = runtime()
  stored.set('stored-2', { header: { id: 'stored-2', cwd: process.cwd(), createdAt: 200 }, events: [] })
  await server.handle('initialize', handshake)
  await server.handle('session/resume', resume)
  await server.handle('session/resume', { ...resume, sessionId: 'stored-2' })
  handles[0].agent.status = 'running'
  await assert.rejects(server.handle('session/history', resume), { kind: 'session/busy' })
  await assert.rejects(server.handle('session/resume', resume), { kind: 'session/busy' })
  assert.equal(reads.length, 0)
  handles[0].agent.status = 'idle'
  const page = await server.handle('session/history', { ...resume, limit: 1 })
  await assert.rejects(server.handle('session/history', { sessionId: 'stored-2', cursor: page.nextCursor }), { kind: 'session/invalid-cursor' })
  await assert.rejects(server.handle('session/list', { cursor: page.nextCursor }), { kind: 'session/invalid-cursor' })
  for (const limit of [null, 0, -1, 257, 1.5, '2']) {
    await assert.rejects(server.handle('session/history', { ...resume, limit }), { code: -32602 })
  }
  await server.shutdown()
})

test('history never skips oversized events or returns a short or noncontiguous prefix', async () => {
  const { server, stored, handles } = runtime()
  await server.handle('initialize', handshake)
  await server.handle('session/resume', resume)
  server.history.maxPageBytes = 120
  const page = await server.handle('session/history', { ...resume, limit: 5 })
  assert.ok(page.events.length > 0 && page.events.length < 5)
  assert.ok(Buffer.byteLength(JSON.stringify(page.events)) <= 120)
  stored.get(resume.sessionId).events[0].data = { text: 'secret'.repeat(100) }
  await assert.rejects(server.handle('session/history', resume), (error) => error.kind === 'session/page-too-large' && !error.message.includes('secret'))
  stored.get(resume.sessionId).events.length = 1
  await assert.rejects(server.handle('session/history', resume), { kind: 'session/history-failed' })
  handles[0].agent.session.seq = 1
  stored.get(resume.sessionId).events[0].seq = 2
  await assert.rejects(server.handle('session/history', resume), { kind: 'session/history-failed' })
  assert.ok(handles.slice(1).every((handle) => handle.closed))
  await server.shutdown()
})

test('catalog and cursor retention are bounded with explicit expiration and restart errors', async () => {
  const { server, stored } = runtime()
  await server.handle('initialize', handshake)
  await server.handle('session/resume', resume)
  const first = await server.handle('session/history', { ...resume, limit: 1 })
  server.history.cursors.get(first.nextCursor).expires = 0
  await assert.rejects(server.handle('session/history', { ...resume, cursor: first.nextCursor }), { kind: 'session/invalid-cursor' })
  const second = await server.handle('session/history', { ...resume, limit: 1 })
  for (let index = 0; index < 33; index++) await server.handle('session/history', { ...resume, limit: 1 })
  assert.equal(server.history.cursors.size, 32)
  await assert.rejects(server.handle('session/history', { ...resume, cursor: second.nextCursor }), { kind: 'session/invalid-cursor' })
  for (let index = 0; index < 1001; index++) stored.set(`many-${index}`, { header: { id: `many-${index}`, createdAt: index }, events: [] })
  await assert.rejects(server.handle('session/list', {}), { kind: 'session/catalog-too-large' })
  await server.shutdown()
  assert.equal(server.history.cursors.size, 0)
})

test('resume reserves ownership, redacts failures, and remains retryable', async () => {
  const { server, ctx, handles } = runtime()
  await server.handle('initialize', handshake)
  const original = ctx.agents.resume
  ctx.agents.resume = async () => { throw new Error('private-token') }
  await assert.rejects(server.handle('session/resume', resume), (error) => error.kind === 'session/resume-failed' && !error.message.includes('private-token'))
  assert.equal(server.sessions.size, 0)
  ctx.agents.resume = original
  server.maxSessions = 0
  await assert.rejects(server.handle('session/resume', resume), { kind: 'session/limit' })
  server.maxSessions = 1
  await server.handle('session/resume', resume)
  assert.equal(handles.length, 1)
  await server.shutdown()
})

test('shutdown aborts in-flight resume and disposes even a late runtime handle', async () => {
  const { server, ctx, handles } = runtime()
  await server.handle('initialize', handshake)
  const original = ctx.agents.resume
  let finish
  let announce
  const started = new Promise((resolve) => { announce = resolve })
  ctx.agents.resume = async (options) => {
    announce()
    await new Promise((resolve) => { finish = resolve })
    assert.equal(options.signal.aborted, true)
    return original(options)
  }
  const restoring = server.handle('session/resume', resume)
  const rejected = assert.rejects(restoring, { kind: 'host/stopping' })
  await started
  await assert.rejects(server.handle('session/resume', resume), { kind: 'session/busy' })
  const shutdown = server.shutdown()
  finish()
  await rejected
  await shutdown
  assert.equal(handles[0].disposed, true)
  assert.equal(server.sessions.size, 0)
})
