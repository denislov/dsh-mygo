import assert from 'node:assert/strict'
import { PassThrough, Writable } from 'node:stream'
import { readFile } from 'node:fs/promises'
import { test } from 'node:test'
import { LineChannel } from '../transport.mjs'
import { MygoServer } from '../server.mjs'

function runtime() {
  const listeners = new Map()
  const handles = []
  const ctx = {
    get(name) { return this[name] },
    on(name, listener) {
      listeners.set(name, listener)
      return () => listeners.delete(name)
    },
    llm: { async resolveCallConfig() {} },
    agents: {
      async create(options) {
        const agent = {
          session: { id: options.sessionId }, status: 'idle',
          followup(message) { this.message = message },
          cancel(cause) { this.cancelCause = cause; this.cancelCount = (this.cancelCount ?? 0) + 1 },
          async whenIdle() { this.status = 'idle' },
        }
        const handle = { agent, async dispose() { handle.disposed = true } }
        handles.push(handle)
        return handle
      },
    },
  }
  const notifications = []
  const server = new MygoServer(ctx, {
    notify: (method, params) => notifications.push({ method, params }),
    createUserMessage: (options) => ({ id: 'message-1', ...options }),
    reasoningEffort: (value) => value,
    dshVersion: '0.2.1-rc.2',
  })
  return { server, ctx, handles, listeners, notifications }
}

const handshake = {
  protocolVersion: 1,
  client: { name: 'dsh-mygo', version: '0.1.0', platform: 'test', locale: 'zh-CN' },
  capabilities: ['approval', 'session.history'],
}

test('handshake gates methods, rejects incompatible versions and never advertises planned capabilities', async () => {
  const { server } = runtime()
  await assert.rejects(server.handle('session/create', {}), { kind: 'protocol/not-initialized' })
  await assert.rejects(server.handle('initialize', { ...handshake, protocolVersion: 2 }), { kind: 'protocol/version-mismatch' })
  const result = await server.handle('initialize', handshake)
  assert.equal(result.protocolVersion, 1)
  assert.equal(result.server.version, '0.1.0')
  assert.deepEqual(result.capabilities, [])
  await assert.rejects(server.handle('initialize', handshake), { kind: 'protocol/already-initialized' })
  await assert.rejects(server.handle('settings/get', {}), { code: -32601 })
  await server.shutdown()
})

test('session/create owns identity, prompt rejects foreign ids and busy turns, events keep durable seq', async () => {
  const { server, handles, listeners, notifications } = runtime()
  await server.handle('initialize', handshake)
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  assert.match(session.id, /^[a-zA-Z0-9-]+$/)
  assert.equal(session.status, 'idle')
  await assert.rejects(server.handle('session/prompt', { sessionId: '../foreign', content: [{ type: 'text', text: 'hi' }] }), { code: -32602 })
  await assert.rejects(server.handle('session/prompt', { sessionId: 'foreign', content: [{ type: 'text', text: 'hi' }] }), { kind: 'session/not-found' })
  assert.deepEqual(await server.handle('session/prompt', { sessionId: session.id, content: [{ type: 'text', text: 'hello' }] }), { messageId: 'message-1' })
  await assert.rejects(server.handle('session/prompt', { sessionId: session.id, content: [{ type: 'text', text: 'again' }] }), { kind: 'session/busy' })
  const event = { seq: 0, type: 'user/message', time: 1, data: { content: [{ type: 'text', text: 'hello' }] } }
  listeners.get('session/event')(handles[0].agent.session, event)
  listeners.get('agent/status')({ agent: handles[0].agent, status: 'running' })
  listeners.get('agent/status')({ agent: handles[0].agent, status: 'idle' })
  assert.deepEqual(notifications.find((entry) => entry.method === 'session/event').params, { sessionId: session.id, seq: 0, event })
  await server.shutdown()
  assert.equal(handles[0].disposed, true)
  assert.equal(listeners.size, 0)
})

test('validation rejects unsupported content and hides runtime exception text', async () => {
  const { server, ctx } = runtime()
  await server.handle('initialize', handshake)
  await assert.rejects(server.handle('session/create', { cwd: 'relative', provider: 'test', model: 'echo' }), { code: -32602 })
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  for (const content of [[], [{ type: 'image', data: 'private' }], [{ type: 'text', text: '' }]]) {
    await assert.rejects(server.handle('session/prompt', { sessionId: session.id, content }), { code: -32602 })
  }
  ctx.agents.create = async () => { throw new Error('private-api-key') }
  await assert.rejects(server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' }), (error) => !error.message.includes('private-api-key'))
  await server.shutdown()
})

test('cancel requires negotiation, validates ownership and does not arm idle sessions', async () => {
  const { server, handles } = runtime()
  await server.handle('initialize', handshake)
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  await assert.rejects(server.handle('session/cancel', { sessionId: session.id }), { kind: 'protocol/capability-unavailable' })
  await server.shutdown()

  const negotiated = runtime()
  const result = await negotiated.server.handle('initialize', { ...handshake, capabilities: ['session.cancel', 'approval'] })
  assert.deepEqual(result.capabilities, ['session.cancel'])
  const created = await negotiated.server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  await assert.rejects(negotiated.server.handle('session/cancel', { sessionId: '../unsafe' }), { code: -32602 })
  await assert.rejects(negotiated.server.handle('session/cancel', { sessionId: 'foreign' }), { kind: 'session/not-found' })
  assert.deepEqual(await negotiated.server.handle('session/cancel', { sessionId: created.session.id }), { cancelled: false, status: 'idle' })
  assert.equal(negotiated.handles[0].agent.cancelCount, undefined)
  assert.equal(handles[0].disposed, true)
  await negotiated.server.shutdown()
})

test('cancel waits for quiescence, coalesces concurrent requests and permits the next prompt', async () => {
  const { server, handles, notifications } = runtime()
  await server.handle('initialize', { ...handshake, capabilities: ['session.cancel'] })
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  const params = { sessionId: session.id }
  await server.handle('session/prompt', { ...params, content: [{ type: 'text', text: 'long turn' }] })
  const agent = handles[0].agent
  agent.status = 'running'
  let finish
  agent.whenIdle = () => new Promise((resolve) => { finish = () => { agent.status = 'idle'; resolve() } })
  const cancelled = server.handle('session/cancel', params)
  const duplicate = server.handle('session/cancel', params)
  assert.equal(agent.cancelCount, 1)
  assert.deepEqual(agent.cancelCause, { kind: 'user' })
  await assert.rejects(server.handle('session/prompt', { ...params, content: [{ type: 'text', text: 'too soon' }] }), { kind: 'session/busy' })
  finish()
  assert.deepEqual(await cancelled, { cancelled: true, status: 'idle' })
  assert.deepEqual(await duplicate, { cancelled: true, status: 'idle' })
  assert.equal(notifications.at(-1).method, 'session/status')
  assert.equal(notifications.at(-1).params.status, 'idle')
  await server.handle('session/prompt', { ...params, content: [{ type: 'text', text: 'next turn' }] })
  assert.equal(agent.message.content[0].text, 'next turn')
  await server.shutdown()
})

test('cancel failures keep the session busy and redact runtime errors', async () => {
  const { server, handles } = runtime()
  await server.handle('initialize', { ...handshake, capabilities: ['session.cancel'] })
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  await server.handle('session/prompt', { sessionId: session.id, content: [{ type: 'text', text: 'long turn' }] })
  handles[0].agent.cancel = () => { throw new Error('private-token') }
  await assert.rejects(server.handle('session/cancel', { sessionId: session.id }), (error) => error.kind === 'session/cancel-failed' && !error.message.includes('private-token'))
  await assert.rejects(server.handle('session/prompt', { sessionId: session.id, content: [{ type: 'text', text: 'too soon' }] }), { kind: 'session/busy' })
  await server.shutdown()
})

test('cancel timeout is bounded and does not report an idle session', async (context) => {
  const { server, handles } = runtime()
  await server.handle('initialize', { ...handshake, capabilities: ['session.cancel'] })
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  await server.handle('session/prompt', { sessionId: session.id, content: [{ type: 'text', text: 'long turn' }] })
  handles[0].agent.whenIdle = () => new Promise(() => {})
  context.mock.timers.enable({ apis: ['setTimeout'] })
  const timedOut = assert.rejects(server.handle('session/cancel', { sessionId: session.id }), { kind: 'session/cancel-timeout' })
  context.mock.timers.tick(5000)
  await timedOut
  await assert.rejects(server.handle('session/prompt', { sessionId: session.id, content: [{ type: 'text', text: 'too soon' }] }), { kind: 'session/busy' })
  await server.shutdown()
})

test('channel returns structured errors before EOF', async () => {
  const input = new PassThrough()
  const output = new PassThrough()
  let wire = ''
  output.on('data', (chunk) => { wire += chunk })
  const channel = new LineChannel(input, output, { maxFrameBytes: 1024 })
  const finished = new Promise((resolve) => {
    channel.start(async (method) => {
      if (method === 'unknown') throw Object.assign(new Error('Unknown method'), { code: -32601, kind: 'protocol/method-not-found' })
      return {}
    }, resolve)
  })
  input.end('{"jsonrpc":"2.0","id":1,"method":"unknown","params":{}}\n')
  await finished
  const response = JSON.parse(wire.trim())
  assert.equal(response.error.code, -32601)
  assert.equal(response.error.data.kind, 'protocol/method-not-found')
})

test('channel bounds output queued behind a slow reader', async () => {
  let finishWrite
  const output = new Writable({ write(_chunk, _encoding, callback) { finishWrite = callback } })
  const channel = new LineChannel(new PassThrough(), output, { maxFrameBytes: 256, maxQueuedBytes: 80 })
  const first = channel.send({ jsonrpc: '2.0', id: 1, result: {} })
  await Promise.resolve()
  await assert.rejects(channel.send({ jsonrpc: '2.0', id: 2, result: 'x'.repeat(40) }))
  assert.equal(channel.closed, true)
  finishWrite()
  await first
})

test('channel serializes requests and flushes both responses', async () => {
  const input = new PassThrough()
  const output = new PassThrough()
  let wire = ''
  output.on('data', (chunk) => { wire += chunk })
  const channel = new LineChannel(input, output, { maxFrameBytes: 1024 })
  const calls = []
  let release
  const blocked = new Promise((resolve) => { release = resolve })
  let entered
  const firstEntered = new Promise((resolve) => { entered = resolve })
  const finished = new Promise((resolve) => channel.start(async (method) => {
    calls.push(method)
    if (method === 'session/create') { entered(); await blocked }
    return {}
  }, resolve))
  input.end('{"jsonrpc":"2.0","id":1,"method":"session/create"}\n{"jsonrpc":"2.0","id":2,"method":"shutdown"}\n')
  await firstEntered
  assert.deepEqual(calls, ['session/create'])
  release()
  await finished
  assert.deepEqual(calls, ['session/create', 'shutdown'])
  assert.deepEqual(wire.trim().split('\n').map((line) => JSON.parse(line).id), [1, 2])
})

test('channel closes on invalid JSON, invalid frames, oversized input and unterminated EOF', async () => {
  const frames = JSON.parse(await readFile(new URL('../../../tests/fixtures/protocol/invalid-frames.json', import.meta.url), 'utf8'))
  for (const frame of [...frames, 'x'.repeat(257)]) {
    const input = new PassThrough()
    const output = new PassThrough()
    const channel = new LineChannel(input, output, { maxFrameBytes: 256 })
    const closed = new Promise((resolve) => channel.start(async () => assert.fail('invalid request dispatched'), resolve))
    input.end(frame)
    assert.ok(await closed)
  }
})
