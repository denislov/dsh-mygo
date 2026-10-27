import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { test } from 'node:test'
import { MygoServer } from '../server.mjs'

const handshake = { protocolVersion: 1, client: { name: 'test', version: '0.1.2', platform: 'test', locale: 'zh-CN' }, capabilities: ['session.permissions'] }
function runtime() {
  const listeners = new Map()
  const specs = { safe: { sandbox: 'workspace-write', approval: 'ask' }, wide: { sandbox: 'danger-full-access', approval: 'never' } }
  const ctx = {
    get(name) { return this[name] }, on(name, fn) { listeners.set(name, fn); return () => listeners.delete(name) },
    llm: { async resolveCallConfig() {} }, sessionPersistence: { async flush() {} },
    permissionPresets: {
      catalog() { return { options: Object.keys(specs).map((value) => ({ value, name: value, description: 'fixture' })) } },
      current(session) { return session.preset ?? 'safe' }, resolve(name) { return specs[name] },
      set(session, preset) {
        if (this.current(session) === preset) return
        session.preset = preset
        for (const [type, data] of [['permission/preset', { preset }], ['sandbox/mode', { mode: specs[preset].sandbox }], ['approval/policy', { policy: specs[preset].approval }]]) {
          session.append(type, data)
        }
      },
    },
    sandboxPolicy: { resolve({ session }) { return { mode: specs[session.preset ?? 'safe'].sandbox, workspaceRoot: 'private-path' } } },
    approval: { effectivePolicy(session) { return specs[session.preset ?? 'safe'].approval } },
    agents: { async create({ sessionId }) {
      const session = { id: sessionId, seq: 0, header: { id: sessionId, createdAt: 1 }, append(type, data) { listeners.get('session/event')(session, { seq: this.seq++, type, data, time: 1 }) } }
      return { agent: { session, status: 'idle' }, async dispose() {} }
    } },
  }
  const server = new MygoServer(ctx, { notify() {}, createUserMessage: (v) => v, dshVersion: '0.2.1-rc.2' })
  return { server, ctx }
}

test('shared permission fixture drives read and select payloads', async () => {
  const fixture = JSON.parse(await readFile(new URL('../../../tests/fixtures/protocol/permission.json', import.meta.url), 'utf8'))
  const { server } = runtime()
  await server.handle('initialize', handshake)
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  const snapshot = await server.handle('permission/get', { ...fixture.get, sessionId: session.id })
  assert.equal(snapshot.current, 'safe')
  await assert.rejects(server.handle('permission/select', { ...fixture.selectSafe, sessionId: session.id, preset: 'wide', expectedNextSeq: 0 }), { kind: 'permission/confirmation-required' })
  const selected = await server.handle('permission/select', { ...fixture.select, sessionId: session.id, preset: 'wide', expectedNextSeq: 0 })
  assert.equal(selected.current, 'wide')
  await server.shutdown()
})

test('permission capability is service gated, snapshot whitelisted and dangerous bundles confirmed', async () => {
  const { server, ctx } = runtime()
  const initialized = await server.handle('initialize', handshake)
  assert.ok(initialized.capabilities.includes('session.permissions'))
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  const initial = await server.handle('permission/get', { sessionId: session.id })
  assert.equal(initial.current, 'safe')
  assert.equal(initial.options[1].requiresConfirmation, true)
  assert.ok(!JSON.stringify(initial).includes('private-path'))
  const params = { sessionId: session.id, preset: 'wide', expectedNextSeq: 0 }
  await assert.rejects(server.handle('permission/select', params), { kind: 'permission/confirmation-required' })
  const result = await server.handle('permission/select', { ...params, confirmDangerous: true })
  assert.equal(result.current, 'wide')
  assert.equal(result.sandbox, 'danger-full-access')
  assert.equal(result.approval, 'never')
  assert.equal(result.nextSeq, 3)
  await assert.rejects(server.handle('permission/select', { ...params, confirmDangerous: true }), { kind: 'permission/conflict' })
  await server.handle('permission/get', { sessionId: session.id })
  const back = await server.handle('permission/select', { ...params, preset: 'safe', expectedNextSeq: 3 })
  assert.equal(back.current, 'safe')
  assert.equal(back.nextSeq, 6)
  for (const extra of [{ preset: 'custom' }, { expectedNextSeq: null }, { confirmDangerous: 'yes' }]) {
    await assert.rejects(server.handle('permission/select', { ...params, ...extra }), { code: -32602 })
  }
  ctx.permissionPresets.set = () => { throw new Error('private-secret') }
  await assert.rejects(server.handle('permission/select', { ...params, expectedNextSeq: 6, confirmDangerous: true }), (e) => e.kind === 'permission/write-failed' && !e.message.includes('private-secret'))
  await assert.rejects(server.handle('permission/select', { ...params, expectedNextSeq: 6, confirmDangerous: true }), { kind: 'permission/refresh-required' })
  await server.shutdown()
  const legacy = runtime()
  delete legacy.ctx.permissionPresets
  assert.ok(!(await legacy.server.handle('initialize', handshake)).capabilities.includes('session.permissions'))
  await assert.rejects(legacy.server.handle('permission/get', {}), { kind: 'protocol/capability-unavailable' })
  await legacy.server.shutdown()
})

test('permission write keeps its reservation until flush and shutdown drains it', async () => {
  const { server, ctx } = runtime()
  await server.handle('initialize', handshake)
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  let finish, entered
  const started = new Promise((resolve) => { entered = resolve })
  ctx.sessionPersistence.flush = () => { entered(); return new Promise((resolve) => { finish = resolve }) }
  const params = { sessionId: session.id, preset: 'wide', expectedNextSeq: 0, confirmDangerous: true }
  const writing = server.handle('permission/select', params)
  await started
  await assert.rejects(server.handle('session/prompt', { sessionId: session.id, content: [{ type: 'text', text: 'hello' }] }), { kind: 'session/busy' })
  await assert.rejects(server.handle('permission/select', params), { kind: 'session/busy' })
  const stopping = server.shutdown()
  finish()
  await writing
  await stopping
})

test('permission reads validate budgets, custom state and actual bundles independently of labels', async () => {
  const { server, ctx } = runtime()
  await server.handle('initialize', handshake)
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  const params = { sessionId: session.id }
  await assert.rejects(server.handle('permission/get', { sessionId: 'foreign' }), { kind: 'session/not-found' })
  const record = server.sessions.get(session.id)
  record.handle.agent.status = 'running'
  await assert.rejects(server.handle('permission/select', { ...params, preset: 'safe', expectedNextSeq: 0 }), { kind: 'session/busy' })
  record.handle.agent.status = 'idle'
  const original = ctx.permissionPresets.catalog
  ctx.permissionPresets.catalog = () => ({ options: [{ value: 'wide', name: 'Looks safe', description: 'private-config' }] })
  ctx.permissionPresets.current = () => 'custom'
  const custom = await server.handle('permission/get', params)
  assert.equal(custom.current, 'custom')
  assert.equal(custom.options[0].requiresConfirmation, true)
  server.permissions.maxBytes = 8
  await assert.rejects(server.handle('permission/get', params), (e) => e.kind === 'permission/unavailable' && !e.message.includes('private-config'))
  server.permissions.maxBytes = 32 * 1024
  ctx.permissionPresets.catalog = () => ({ options: Array.from({ length: 33 }, () => original().options[0]) })
  await assert.rejects(server.handle('permission/get', params), { kind: 'permission/unavailable' })
  await server.shutdown()
})
