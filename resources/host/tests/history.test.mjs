import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { test } from 'node:test'
import { MygoServer } from '../server.mjs'
import { AssistantStreamBaseline } from '../stream-baseline.mjs'

const handshake = {
  protocolVersion: 1,
  client: { name: 'dsh-mygo', version: '0.1.1', platform: 'test', locale: 'zh-CN' },
  capabilities: ['session.history'],
}

function runtime(management = false) {
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
  if (management) {
    const archived = new Set()
    ctx.workspaceRegistry = {
      get archivedSessionIds() { return [...archived] },
      async archiveSession(id) { archived.add(id) },
      async unarchiveSession(id) { archived.delete(id) },
    }
    ctx.sessionTitle = {
      rename(session, title) {
        const event = { seq: session.seq++, time: 500, type: 'session/title', data: { title: title.trim(), source: { kind: 'user' }, messageSeqs: [] } }
        stored.get(session.id).events.push(event)
        return { title: event.data.title }
      },
    }
  }
  const server = new MygoServer(ctx, { notify() {}, createUserMessage: (value) => value, reasoningEffort: (value) => value, dshVersion: '0.2.1-rc.2' })
  return { server, ctx, stored, reads, handles }
}

const resume = { sessionId: 'stored-1', provider: 'test', model: 'echo' }

test('backward windows read a large tail, keep the cut and bind direction', async () => {
  const fixture = JSON.parse(await readFile(new URL('../../../tests/fixtures/protocol/session.json', import.meta.url), 'utf8'))
  const { server, stored } = runtime()
  await server.handle('initialize', { ...handshake, capabilities: ['session.history', 'session.history-window'] })
  const source = stored.get(resume.sessionId)
  source.events = Array.from({ length: 25001 }, (_, seq) => ({ seq, time: seq, type: 'test/event', data: {} }))
  await server.handle('session/resume', { ...fixture.resumeExplicit, sessionId: resume.sessionId })
  const first = await server.handle('session/history', { ...resume, direction: 'backward', limit: 2 })
  assert.equal(first.startSeq, 24999)
  assert.equal(first.nextSeq, 25001)
  assert.deepEqual(first.events.map(e => e.seq), [24999, 25000])
  source.events.push({ seq: 25001, time: 25001, type: 'test/event', data: {} })
  server.sessions.get(resume.sessionId).handle.agent.session.seq++
  const second = await server.handle('session/history', { ...resume, direction: 'backward', cursor: first.nextCursor, limit: 2 })
  assert.equal(second.nextSeq, 25001)
  assert.deepEqual(second.events.map(e => e.seq), [24997, 24998])
  const anchored = await server.handle('session/history', { ...resume, direction: 'backward', beforeSeq: 2, limit: 2 })
  assert.deepEqual(anchored.events.map(e => e.seq), [0, 1])
  assert.equal(anchored.hasMore, false)
  await assert.rejects(server.handle('session/history', { ...resume, cursor: first.nextCursor }), { kind: 'session/invalid-cursor' })
  for (const beforeSeq of [null, -1, 25003, '2', 0.5]) await assert.rejects(server.handle('session/history', { ...resume, direction: 'backward', beforeSeq }), { code: -32602 })
  server.history.maxPageBytes = Buffer.byteLength(JSON.stringify(source.events.at(-1))) + 3
  const small = await server.handle('session/history', { ...resume, direction: 'backward', limit: 2 })
  assert.deepEqual(small.events.map(e => e.seq), [25001])
  await server.shutdown()
})

test('release is negotiated, waits for disposal and reserves against concurrent use', async () => {
  const { server, ctx, stored, handles } = runtime()
  await server.handle('initialize', { ...handshake, capabilities: ['session.history', 'session.release'] })
  server.maxSessions = 1
  for (let index = 0; index < 12; index++) {
    const id = 'many-' + index
    stored.set(id, { header: { id, cwd: process.cwd(), createdAt: index }, events: [] })
    await server.handle('session/resume', { ...resume, sessionId: id })
    await server.handle('session/release', { sessionId: id })
    assert.equal(server.sessions.size, 0)
    assert.ok(stored.has(id))
  }
  await server.handle('session/resume', resume)
  const handle = server.sessions.get(resume.sessionId).handle
  handle.agent.status = 'running'
  await assert.rejects(server.handle('session/release', resume), { kind: 'session/busy' })
  handle.agent.status = 'idle'
  let finish
  handle.dispose = () => new Promise(resolve => { finish = resolve })
  const releasing = server.handle('session/release', resume)
  await new Promise(resolve => setImmediate(resolve))
  for (const method of ['session/resume', 'session/history', 'session/prompt']) await assert.rejects(server.handle(method, { ...resume, content: [{ type: 'text', text: 'hello' }] }), { kind: 'session/busy' })
  assert.equal(server.sessions.size, 1)
  finish()
  assert.deepEqual(await releasing, {})
  assert.deepEqual(await server.handle('session/release', resume), {})
  assert.ok(handles.every(h => h.disposed || h === handle))
  await server.shutdown()
})

test('indexed catalog never opens bodies; missing and stale indexes are explicit hints', async () => {
  const { server, ctx, reads } = runtime()
  ctx.sessionProjections = { snapshot: () => ({ values: { mygoCatalog: { title: 'Live', updatedAt: 800 } } }) }
  ctx.sessionProjectionCache = { cachedSnapshot: () => ({ asOfSeq: 2, values: { mygoCatalog: { title: 'Indexed', updatedAt: 700 } } }) }
  await server.handle('initialize', handshake)
  const first = await server.handle('session/list', {})
  assert.equal(first.sessions[0].title, 'Indexed')
  assert.equal(reads.length, 0)
  ctx.sessionProjectionCache.cachedSnapshot = () => undefined
  const missing = await server.handle('session/list', {})
  assert.equal(missing.sessions[0].metadataIncomplete, true)
  assert.equal(reads.length, 0)
  ctx.sessionProjectionCache.cachedSnapshot = () => { throw new Error('private-token') }
  assert.equal((await server.handle('session/list', {})).sessions[0].metadataIncomplete, true)
  await server.shutdown()
})

test('release failures remain reserved and retryable; readers and questions block release', async () => {
  const { server, ctx } = runtime()
  await server.handle('initialize', { ...handshake, capabilities: ['session.history', 'session.release'] })
  await server.handle('session/resume', resume)
  const record = server.sessions.get(resume.sessionId)
  for (const key of ['historyReaders', 'catalogReaders', 'modelUpdating', 'metadataUpdating', 'cancelTask']) {
    record[key] = true
    await assert.rejects(server.handle('session/release', resume), { kind: 'session/busy' })
    record[key] = false
  }
  server.questions.pending.set('question', { sessionId: resume.sessionId })
  await assert.rejects(server.handle('session/release', resume), { kind: 'session/busy' })
  server.questions.pending.clear()
  const dispose = record.handle.dispose
  record.handle.dispose = async () => { throw new Error('private-token') }
  await assert.rejects(server.handle('session/release', resume), error => error.kind === 'session/release-failed' && !error.message.includes('private-token'))
  assert.equal(server.sessions.get(resume.sessionId), record)
  await assert.rejects(server.handle('session/resume', resume), { kind: 'session/busy' })
  record.handle.dispose = dispose
  await server.handle('session/release', resume)
  assert.equal(server.sessions.size, 0)
  await server.shutdown()
})

test('cold acquisition stays reserved while its index checkpoint settles', async () => {
  const { server, ctx } = runtime()
  await server.handle('initialize', { ...handshake, capabilities: ['session.history', 'session.release'] })
  let announce, finish
  const started = new Promise(resolve => { announce = resolve })
  ctx.sessionProjectionCache = { write: async () => { announce(); await new Promise(resolve => { finish = resolve }) } }
  const opening = server.handle('session/resume', resume)
  await started
  for (const method of ['session/release', 'session/history', 'session/resume']) await assert.rejects(server.handle(method, resume), { kind: 'session/busy' })
  finish()
  await opening
  ctx.sessionProjectionCache.write = async () => {}
  await server.handle('session/release', resume)
  await server.shutdown()
})

test('release refuses idle parents with active jobs or subagents without stopping them', async () => {
  const { server, ctx } = runtime()
  await server.handle('initialize', { ...handshake, capabilities: ['session.history', 'session.release'] })
  await server.handle('session/resume', resume)
  let drained = false
  ctx.subagents = { async drainDescendants() { drained = true } }
  ctx.waterfall = async (method, params, next) => {
    assert.equal(method, 'workspace/session-activity')
    assert.equal(params.sessionId, resume.sessionId)
    assert.deepEqual(await next(), [])
    return [{ kind: 'job', items: [] }]
  }
  await assert.rejects(server.handle('session/release', resume), { kind: 'session/busy' })
  assert.equal(drained, false)
  assert.equal(server.sessions.get(resume.sessionId).metadataUpdating, false)
  assert.equal((await server.handle('session/resume', resume)).session.id, resume.sessionId)
  ctx.waterfall = async () => []
  await server.handle('session/release', resume)
  assert.equal(drained, true)
  await server.shutdown()
})

test('stream baseline shares the history cut, stays fixed across pages and requires negotiation', async () => {
  const { server, ctx } = runtime()
  const capabilities = ['session.history', 'session.resync', 'session.assistant-stream', 'session.assistant-stream-baseline']
  await server.handle('initialize', { ...handshake, capabilities })
  assert.ok(server.capabilities.includes('session.assistant-stream-baseline'))
  await server.handle('session/resume', resume)
  const stream = new AssistantStreamBaseline()
  server.sessions.get(resume.sessionId).assistantStream = stream
  stream.accept({ type: 'start', revision: 1, attemptId: 's:1', turn: 1, step: 1 })
  // flush yields after the synchronous boundary; subsequent changes cannot leak.
  ctx.sessionPersistence.flush = async () => { stream.accept({ type: 'end', revision: 2, attemptId: 's:1', index: 0 }) }
  const params = { ...resume, live: true, assistantStream: true, limit: 2 }
  const first = await server.handle('session/history', params)
  assert.equal(first.assistantStream.revision, 1)
  assert.equal(first.assistantStream.activeAttempt.attemptId, 's:1')
  const second = await server.handle('session/history', { ...params, cursor: first.nextCursor })
  assert.deepEqual(second.assistantStream, first.assistantStream)
  await assert.rejects(server.handle('session/history', { ...params, assistantStream: false, cursor: first.nextCursor }), { kind: 'session/invalid-cursor' })
  const plain = await server.handle('session/history', { ...resume, live: true })
  assert.equal(plain.assistantStream, undefined)
  await assert.rejects(server.handle('session/history', { ...params, assistantStream: null }), { code: -32602 })
  stream.accept({ type: 'start', revision: 3, attemptId: 's:2', turn: 2, step: 1 })
  server.history.maxPageBytes = Buffer.byteLength(JSON.stringify(stream.snapshot())) + Buffer.byteLength(JSON.stringify(first.events[0])) + 3
  const budgeted = await server.handle('session/history', params)
  assert.equal(budgeted.events.length, 1)
  assert.ok(Buffer.byteLength(JSON.stringify(budgeted.events)) + Buffer.byteLength(JSON.stringify(budgeted.assistantStream)) <= server.history.maxPageBytes)
  server.history.maxPageBytes = 8
  await assert.rejects(server.handle('session/history', params), { kind: 'session/page-too-large' })
  await server.shutdown()
  const legacy = runtime().server
  await legacy.handle('initialize', { ...handshake, capabilities: capabilities.filter((value) => value !== 'session.resync') })
  assert.ok(!legacy.capabilities.includes('session.assistant-stream-baseline'))
  await assert.rejects(legacy.handle('session/history', { ...resume, assistantStream: true }), { kind: 'protocol/capability-unavailable' })
  await legacy.shutdown()
})

test('reasoning baseline requires its own negotiated capability', async () => {
  for (const reasoning of [true, false]) {
    const { server } = runtime()
    const capabilities = ['session.history', 'session.resync', 'session.assistant-stream', 'session.assistant-stream-baseline', ...(reasoning ? ['session.assistant-stream-baseline-reasoning'] : [])]
    await server.handle('initialize', { ...handshake, capabilities })
    assert.equal(server.capabilities.includes('session.assistant-stream-baseline-reasoning'), reasoning)
    await server.handle('session/resume', resume)
    const stream = new AssistantStreamBaseline()
    server.sessions.get(resume.sessionId).assistantStream = stream
    stream.accept({ type: 'start', revision: 1, attemptId: 's:1', turn: 1, step: 1 })
    stream.accept({ type: 'chunk', revision: 2, attemptId: 's:1', index: 0, chunk: { type: 'block-start', index: 0, blockType: 'reasoning' } })
    stream.accept({ type: 'chunk', revision: 3, attemptId: 's:1', index: 1, chunk: { type: 'reasoning-delta', index: 0, text: 'plan' } })
    stream.accept({ type: 'chunk', revision: 4, attemptId: 's:1', index: 2, chunk: { type: 'block-start', index: 1, blockType: 'text' } })
    stream.accept({ type: 'chunk', revision: 5, attemptId: 's:1', index: 3, chunk: { type: 'text-delta', index: 1, text: 'answer' } })
    const result = await server.handle('session/history', { ...resume, live: true, assistantStream: true })
    const blocks = result.assistantStream.activeAttempt.blocks
    assert.equal(blocks[0].type, 'reasoning')
    assert.equal(blocks[0].text, reasoning ? 'plan' : undefined)
    assert.equal(blocks[1].text, 'answer')
    await server.shutdown()
  }
})

test('live history is negotiated and holds one sequence and status cut while the agent runs', async () => {
  const { server, stored, handles } = runtime()
  await server.handle('initialize', { ...handshake, capabilities: ['session.history', 'session.resync'] })
  assert.ok(server.capabilities.includes('session.resync'))
  await server.handle('session/resume', resume)
  const record = server.sessions.get(resume.sessionId)
  handles[0].agent.status = 'running'
  record.statusRevision = 3
  const first = await server.handle('session/history', { ...resume, live: true, limit: 2 })
  assert.equal(first.status, 'running')
  assert.equal(first.statusRevision, 3)
  stored.get(resume.sessionId).events.push({ seq: 5, time: 6, type: 'turn/end', data: {} })
  handles[0].agent.session.seq = 6
  handles[0].agent.status = 'idle'
  record.statusRevision = 4
  const second = await server.handle('session/history', { ...resume, live: true, limit: 2, cursor: first.nextCursor })
  const last = await server.handle('session/history', { ...resume, live: true, limit: 2, cursor: second.nextCursor })
  assert.equal(last.nextSeq, 5)
  assert.equal(last.status, 'running')
  assert.equal(last.statusRevision, 3)
  assert.deepEqual(last.events.map(({ seq }) => seq), [4])
  await assert.rejects(server.handle('session/history', { ...resume, cursor: first.nextCursor }), { kind: 'session/invalid-cursor' })
  await server.shutdown()
})

test('legacy history keeps its busy rule and cannot request an unnegotiated live snapshot', async () => {
  const { server } = runtime()
  await server.handle('initialize', handshake)
  await server.handle('session/resume', resume)
  await assert.rejects(server.handle('session/history', { ...resume, live: true }), { kind: 'protocol/capability-unavailable' })
  for (const live of [null, 1, 'true']) await assert.rejects(server.handle('session/history', { ...resume, live }), { code: -32602 })
  await server.shutdown()
})

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

test('session management negotiates each available service independently', async () => {
  for (const enabled of [false, true]) {
    const { server, ctx } = runtime(enabled)
    const capabilities = ['session.history', 'session.rename', 'session.archive']
    assert.deepEqual((await server.handle('initialize', { ...handshake, capabilities })).capabilities, enabled ? capabilities : ['session.history'])
    if (!enabled) {
      for (const method of ['session/rename', 'session/archive']) await assert.rejects(server.handle(method, { ...resume, title: 'New title' }), { kind: 'protocol/capability-unavailable' })
    }
    await server.shutdown()
    if (enabled) {
      const other = runtime(true)
      delete other.ctx.sessionTitle
      assert.deepEqual((await other.server.handle('initialize', { ...handshake, capabilities })).capabilities, ['session.history', 'session.archive'])
      await other.server.shutdown()
    }
  }
})

const managementHandshake = { ...handshake, capabilities: ['session.history', 'session.rename', 'session.archive'] }

test('rename commits a user title and list reads durable metadata sorted by activity', async () => {
  const { server, stored } = runtime(true)
  stored.set('newer', { header: { id: 'newer', cwd: process.cwd(), createdAt: 200 }, events: [] })
  await server.handle('initialize', managementHandshake)
  await server.handle('session/resume', resume)
  const renamed = await server.handle('session/rename', { sessionId: resume.sessionId, title: '  工作会话  ' })
  assert.equal(renamed.session.title, '工作会话')
  assert.equal(renamed.session.updatedAt, 500)
  assert.equal(renamed.session.nextSeq, 6)
  const result = await server.handle('session/list', {})
  assert.deepEqual(result.sessions.map(({ id }) => id), ['stored-1', 'newer'])
  assert.equal(result.sessions[0].title, '工作会话')
  assert.equal(result.sessions[1].title, 'newer')
  assert.equal(stored.get(resume.sessionId).events.at(-1).data.source.kind, 'user')
  await server.shutdown()
})

test('archive hides stored sessions by default and keeps an explicit archived listing', async () => {
  const { server, stored } = runtime(true)
  const original = structuredClone(stored.get(resume.sessionId).events)
  await server.handle('initialize', managementHandshake)
  await server.handle('session/resume', resume)
  assert.deepEqual(await server.handle('session/archive', { sessionId: resume.sessionId }), {})
  assert.deepEqual((await server.handle('session/list', {})).sessions, [])
  const all = await server.handle('session/list', { includeArchived: true })
  assert.equal(all.sessions[0].archived, true)
  assert.deepEqual(stored.get(resume.sessionId).events, original)
  assert.deepEqual(await server.handle('session/archive', { sessionId: resume.sessionId }), {})
  await assert.rejects(server.handle('session/resume', resume), { kind: 'session/archived' })
  await assert.rejects(server.handle('session/prompt', { sessionId: resume.sessionId, content: [{ type: 'text', text: 'hello' }] }), { kind: 'session/archived' })
  await server.handle('session/archive', { sessionId: resume.sessionId, archived: false })
  assert.equal((await server.handle('session/list', {})).sessions[0].archived, false)
  await server.handle('session/resume', resume)
  await server.shutdown()
})

test('management validates input, refuses foreign and busy sessions, and redacts storage errors', async () => {
  const { server, ctx, handles } = runtime(true)
  await server.handle('initialize', managementHandshake)
  for (const title of ['', '   ', null, 1, 'x\nsecret', 'x\0', '中'.repeat(342)]) {
    await assert.rejects(server.handle('session/rename', { sessionId: resume.sessionId, title }), { code: -32602 })
  }
  for (const method of ['session/rename', 'session/archive']) {
    await assert.rejects(server.handle(method, { sessionId: '../private', title: 'title' }), { code: -32602 })
    await assert.rejects(server.handle(method, { sessionId: 'missing', title: 'title' }), { kind: 'session/not-found' })
  }
  await assert.rejects(server.handle('session/rename', { sessionId: resume.sessionId, title: 'title' }), { kind: 'session/not-found' })
  for (const includeArchived of [null, 1, 'true']) await assert.rejects(server.handle('session/list', { includeArchived }), { code: -32602 })
  for (const archived of [null, 1, 'true']) await assert.rejects(server.handle('session/archive', { sessionId: resume.sessionId, archived }), { code: -32602 })
  await server.handle('session/resume', resume)
  handles[0].agent.status = 'running'
  for (const method of ['session/rename', 'session/archive']) await assert.rejects(server.handle(method, { sessionId: resume.sessionId, title: 'title' }), { kind: 'session/busy' })
  handles[0].agent.status = 'idle'
  ctx.sessionTitle.rename = () => { throw new Error('private-token') }
  ctx.workspaceRegistry.archiveSession = async () => { throw new Error('private-token') }
  for (const method of ['session/rename', 'session/archive']) await assert.rejects(server.handle(method, { sessionId: resume.sessionId, title: 'title' }), (error) => error.kind.endsWith('-failed') && !error.message.includes('private-token'))
  assert.equal(server.sessions.get(resume.sessionId).metadataUpdating, false)
  await server.shutdown()
})

test('pending metadata writes reserve the session until durability and shutdown waits', async () => {
  const { server, ctx } = runtime(true)
  await server.handle('initialize', managementHandshake)
  await server.handle('session/resume', resume)
  let finish
  let announce
  const started = new Promise((resolve) => { announce = resolve })
  ctx.sessionPersistence.flush = async () => { announce(); await new Promise((resolve) => { finish = resolve }) }
  const writing = server.handle('session/rename', { sessionId: resume.sessionId, title: 'Pending' })
  await started
  for (const method of ['session/rename', 'session/archive', 'session/resume', 'session/history', 'session/prompt']) {
    await assert.rejects(server.handle(method, { ...resume, title: 'other', content: [{ type: 'text', text: 'hello' }] }), { kind: 'session/busy' })
  }
  let stopped = false
  const stopping = server.shutdown().then(() => { stopped = true })
  await Promise.resolve()
  assert.equal(stopped, false)
  finish()
  await writing
  await stopping
  assert.equal(stopped, true)
})

test('list metadata caches revisions, invalidates changes, and closes failed reads', async () => {
  const { server, stored, handles, reads } = runtime()
  const source = stored.get(resume.sessionId)
  source.revision = 'first'
  // Expose detached persistence metadata, as a real backend does.
  server.ctx.sessionPersistence.list = async () => [...stored.values()].map(({ header, events, revision }) => ({ header, eventCount: events.length, revision }))
  await server.handle('initialize', handshake)
  await server.handle('session/list', {})
  const count = reads.length
  await server.handle('session/list', {})
  assert.equal(reads.length, count)
  source.events.push({ seq: 5, time: 700, type: 'session/title', data: { title: 'Persisted title' } })
  source.revision = 'second'
  const changed = await server.handle('session/list', {})
  assert.equal(changed.sessions[0].title, 'Persisted title')
  assert.equal(changed.sessions[0].updatedAt, 700)
  assert.ok(reads.length > count)
  assert.ok(handles.every((handle) => handle.closed))
  source.revision = 'invalid'
  source.events[0].seq = 8
  await assert.rejects(server.handle('session/list', {}), { kind: 'session/list-failed' })
  assert.equal(handles.at(-1).closed, true)
  await server.shutdown()
  assert.equal(server.history.metadata.size, 0)
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
