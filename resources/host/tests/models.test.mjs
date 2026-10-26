import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { test } from 'node:test'
import { MygoServer } from '../server.mjs'

const handshake = { protocolVersion: 1, client: { name: 'dsh-mygo', version: '0.1.1', platform: 'test', locale: 'zh-CN' }, capabilities: ['model.selection'] }

async function runtime(capabilities = handshake.capabilities) {
  const listeners = new Map()
  const notifications = []
  let installed
  const ctx = {
    get(name) { return this[name] },
    on(name, listener) { listeners.set(name, listener); return () => listeners.delete(name) },
    llm: {
      listProviders() { return [{ id: 'test', name: 'Test', key: 'private-token' }] },
      async listModels() { return [{ id: 'echo', name: 'Echo', provider: 'test', secret: 'private-token' }, { id: 'other', name: 'Other', provider: 'test' }] },
      async resolveModelInfo() { return { reasoning: { efforts: [{ id: 'low', name: 'Low' }, { id: 'high', name: 'High' }] }, secret: 'private-token' } },
      async resolveCallConfig(options) { if (options.model === 'bad') throw new Error('private-token') },
    },
    agents: {
      async create(options) {
        const session = { id: options.sessionId, seq: 0, header: { id: options.sessionId, createdAt: 1, cwd: process.cwd() }, append(type, data) {
          const event = { seq: this.seq++, time: 1, type, data }
          listeners.get('session/event')(session, event)
        } }
        options.setup?.({}, { session })
        return { agent: { session, status: 'idle' }, async dispose() {} }
      },
    },
  }
  const server = new MygoServer(ctx, { notify: (method, params) => notifications.push({ method, params }), createUserMessage: (value) => value, reasoningEffort: (value) => value, installModelSelection: (_ctx, selection) => { installed = selection }, dshVersion: '0.2.1-rc.2' })
  const initialized = await server.handle('initialize', { ...handshake, capabilities })
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  return { ctx, server, session, initialized, notifications, selection: () => installed }
}

test('model capability gates methods and catalog only returns display metadata', async () => {
  const disabled = await runtime([])
  for (const method of ['model/list', 'model/select']) await assert.rejects(disabled.server.handle(method, { sessionId: disabled.session.id }), { kind: 'protocol/capability-unavailable' })
  await disabled.server.shutdown()
  const { server, session, initialized } = await runtime()
  assert.deepEqual(initialized.capabilities, ['model.selection'])
  const catalog = await server.handle('model/list', { sessionId: session.id })
  assert.equal(catalog.provider, 'test')
  assert.equal(catalog.models[0].efforts[1].id, 'high')
  assert.deepEqual(catalog.selection, { provider: 'test', model: 'echo', effort: '' })
  assert.ok(!JSON.stringify(catalog).includes('private-token'))
  await assert.rejects(server.handle('model/list', { sessionId: 'foreign' }), { kind: 'session/not-found' })
  await server.shutdown()
})

test('shared model fixture drives list and select payloads including empty effort', async () => {
  const fixture = JSON.parse(await readFile(new URL('../../../tests/fixtures/protocol/model.json', import.meta.url), 'utf8'))
  const { server, session, selection } = await runtime()
  const catalog = await server.handle('model/list', { ...fixture.list, sessionId: session.id })
  assert.equal(catalog.provider, fixture.catalog.provider)
  assert.deepEqual(catalog.selection, fixture.catalog.selection)
  const selected = await server.handle('model/select', { ...fixture.selectEffort, sessionId: session.id, model: 'other' })
  assert.equal(selected.session.model, 'other')
  assert.equal(selection().current.reasoningEffort, 'high')
  await server.handle('model/select', { ...fixture.select, sessionId: session.id, model: 'other' })
  assert.equal(selection().current.reasoningEffort, undefined)
  await server.shutdown()
})

test('model selection commits durable intent before changing the installed selection', async () => {
  const { server, session, notifications, selection } = await runtime()
  const selected = await server.handle('model/select', { sessionId: session.id, provider: 'test', model: 'other', effort: 'high' })
  assert.equal(selected.session.model, 'other')
  assert.equal(selected.session.nextSeq, 1)
  assert.deepEqual(selection().current, { provider: 'test', model: 'other', reasoningEffort: 'high' })
  assert.equal(notifications[0].params.event.type, 'model/selection')
  await assert.rejects(server.handle('model/select', { sessionId: session.id, provider: 'test', model: 'bad' }), (error) => error.kind === 'model/select-failed' && !error.message.includes('private-token'))
  assert.equal(selection().current.model, 'other')
  await server.handle('model/select', { sessionId: session.id, provider: 'test', model: 'other', effort: '' })
  assert.equal(selection().current.reasoningEffort, undefined)
  assert.equal(notifications.at(-1).params.event.data.reasoningEffort, undefined)
  await server.shutdown()
})

test('model requests validate ids, route fields and idle ownership', async () => {
  const { server, session } = await runtime()
  for (const sessionId of ['../bad', '', 'bad\n']) await assert.rejects(server.handle('model/list', { sessionId }), { code: -32602 })
  for (const provider of [null, '', 'x'.repeat(257)]) await assert.rejects(server.handle('model/select', { sessionId: session.id, provider, model: 'echo' }), { code: -32602 })
  server.sessions.get(session.id).busy = true
  await assert.rejects(server.handle('model/select', { sessionId: session.id, provider: 'test', model: 'other' }), { kind: 'session/busy' })
  await server.shutdown()
})

test('model catalog exposes sanitized input modalities', async () => {
  const { ctx, server, session } = await runtime()
  ctx.llm.resolveModelInfo = async () => ({ reasoning: { efforts: [] }, inputModalities: ['text', 'image', 'image'] })
  const catalog = await server.handle('model/list', { sessionId: session.id })
  assert.deepEqual(catalog.models[0].inputModalities, ['text', 'image'])
  ctx.llm.resolveModelInfo = async () => ({ reasoning: { efforts: [] }, inputModalities: ['video'] })
  const invalid = await server.handle('model/list', { sessionId: session.id })
  assert.equal(invalid.models[0].available, false)
  assert.equal(invalid.models[0].inputModalities, undefined)
  ctx.llm.resolveModelInfo = async () => ({ reasoning: { efforts: [] } })
  const unknown = await server.handle('model/list', { sessionId: session.id })
  assert.equal(unknown.models[0].inputModalities, undefined)
  await server.shutdown()
})

test('catalog failures are explicit and bounded without exposing adapter errors', async () => {
  const { ctx, server, session } = await runtime()
  ctx.llm.listModels = async () => { throw new Error('private-token') }
  const unavailable = await server.handle('model/list', { sessionId: session.id })
  assert.equal(unavailable.available, false)
  assert.ok(!JSON.stringify(unavailable).includes('private-token'))
  ctx.llm.listProviders = () => Array.from({ length: 33 }, (_, index) => ({ id: `p${index}`, name: `Provider ${index}` }))
  await assert.rejects(server.handle('model/list', { sessionId: session.id }), { kind: 'model/catalog-too-large' })
  await server.shutdown()
})

test('model updates reserve idle ownership and failures never overwrite the active route', async () => {
  const { ctx, server, session, selection } = await runtime()
  let finish
  ctx.llm.resolveCallConfig = () => new Promise((resolve) => { finish = resolve })
  const updating = server.handle('model/select', { sessionId: session.id, provider: 'test', model: 'other' })
  await assert.rejects(server.handle('session/prompt', { sessionId: session.id, content: [{ type: 'text', text: 'no' }] }), { kind: 'session/busy' })
  await assert.rejects(server.handle('model/select', { sessionId: session.id, provider: 'test', model: 'echo' }), { kind: 'session/busy' })
  assert.equal(selection().current, undefined)
  finish()
  await updating
  assert.equal(selection().current.model, 'other')
  server.sessions.get(session.id).handle.agent.session.append = () => { throw new Error('private-token') }
  ctx.llm.resolveCallConfig = async () => {}
  await assert.rejects(server.handle('model/select', { sessionId: session.id, provider: 'test', model: 'echo' }), (error) => error.kind === 'model/select-failed' && !error.message.includes('private-token'))
  assert.equal(selection().current.model, 'other')
  await server.shutdown()
})

test('model validation timeout releases its reservation and shutdown prevents late commits', async (context) => {
  const { ctx, server, session, notifications, selection } = await runtime()
  ctx.llm.resolveCallConfig = () => new Promise(() => {})
  context.mock.timers.enable({ apis: ['setTimeout'] })
  const timedOut = assert.rejects(server.handle('model/select', { sessionId: session.id, provider: 'test', model: 'other' }), { kind: 'model/select-failed' })
  context.mock.timers.tick(5000)
  await timedOut
  assert.equal(server.sessions.get(session.id).modelUpdating, false)
  assert.equal(selection().current, undefined)
  let finish
  ctx.llm.resolveCallConfig = () => new Promise((resolve) => { finish = resolve })
  const stopped = assert.rejects(server.handle('model/select', { sessionId: session.id, provider: 'test', model: 'other' }), { kind: 'model/select-failed' })
  await server.shutdown()
  finish()
  await stopped
  assert.equal(notifications.length, 0)
})

test('metadata failures are reported per model and serialized output respects the frame budget', async () => {
  const { ctx, server, session } = await runtime()
  ctx.llm.resolveModelInfo = async () => { throw new Error('private-token') }
  const catalog = await server.handle('model/list', { sessionId: session.id })
  assert.equal(catalog.models[0].available, false)
  assert.ok(!JSON.stringify(catalog).includes('private-token'))
  server.models.maxBytes = 100
  await assert.rejects(server.handle('model/list', { sessionId: session.id }), { kind: 'model/catalog-too-large' })
  ctx.llm.listProviders = () => { throw Object.assign(new Error('private-token'), { kind: 'model/catalog-too-large' }) }
  await assert.rejects(server.handle('model/list', { sessionId: session.id }), (error) => !error.message.includes('private-token'))
  await server.shutdown()
})
