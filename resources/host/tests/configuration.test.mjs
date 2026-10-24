import assert from 'node:assert/strict'
import { test } from 'node:test'
import { MygoServer } from '../server.mjs'

function runtime(capabilities = ['settings.configuration']) {
  let revision = 0
  let route = { provider: 'test', model: 'echo', reasoningEffort: 'high' }
  let stored
  const references = []
  const ctx = {
    get(name) { return this[name] }, on() { return () => {} },
    agentDefaultModel: { currentSelection: () => ({ ...route }) },
    settings: {
      describe(options) {
        assert.equal(options.redactSecrets, true)
        return [{ ns: 'agent-default-model', revision, value: { ...route } }, { ns: 'llm-deepseek', revision: 0, value: { apiKeyEnv: 'MYGO_TEST_KEY', secret: 'never-forward' } }]
      },
      async replace(ns, next, expected) {
        assert.equal(ns, 'agent-default-model')
        if (expected !== revision) throw Object.assign(new Error('never-forward'), { code: 'SETTINGS_CONFLICT' })
        route = { ...next }; revision++
      },
    },
    credentials: {
      async describe(ref) { references.push(ref); return { configured: stored !== undefined, writable: true, source: stored ? 'file' : undefined, value: 'never-forward' } },
      async set(ref, value) { references.push(ref); stored = value },
      async unset(ref) { references.push(ref); stored = undefined },
    },
    llm: {
      listConfigurableProviders: () => [{ provider: 'deepseek-official', settingsNs: 'llm-deepseek', settingsPath: [] }],
      async resolveCallConfig(options) { if (options.model === 'bad') throw new Error('never-forward') },
    },
    agents: { async create(options) { return { agent: { status: 'idle', session: { id: options.sessionId, seq: 0 } }, async dispose() {} } } },
  }
  const server = new MygoServer(ctx, { notify() {}, createUserMessage: (value) => value, reasoningEffort: (value) => value, credentialRef: (value) => value, dshVersion: '0.2.1-rc.2' })
  const initialized = server.initialize({ protocolVersion: 1, client: { name: 'test', version: '0.1.1', platform: 'test', locale: 'en' }, capabilities })
  return { server, ctx, initialized, references }
}

test('configuration is capability gated and status only returns whitelisted facts', async () => {
  const disabled = runtime([])
  await assert.rejects(disabled.server.handle('configuration/get', {}), { kind: 'protocol/capability-unavailable' })
  const { server, initialized } = runtime()
  assert.ok(initialized.capabilities.includes('settings.configuration'))
  const snapshot = await server.handle('configuration/get', {})
  assert.deepEqual(snapshot.defaults, { provider: 'test', model: 'echo', effort: 'high' })
  assert.deepEqual(snapshot.credential, { available: true, reference: 'MYGO_TEST_KEY', configured: false, writable: true, source: 'none' })
  assert.ok(!JSON.stringify(snapshot).includes('never-forward'))
})

test('default model persists with revision checks, clears effort, and leaves existing sessions unchanged', async () => {
  const { server, ctx } = runtime()
  const { session } = await server.handle('session/create', { cwd: process.cwd(), useDefaultModel: true })
  const saved = await server.handle('configuration/default-model', { revision: 0, provider: 'test', model: 'next', effort: '' })
  assert.equal(saved.defaults.model, 'next')
  assert.equal(saved.defaults.effort, '')
  assert.equal(ctx.agentDefaultModel.currentSelection().reasoningEffort, undefined)
  assert.equal(server.sessions.get(session.id).route.model, 'echo')
  await assert.rejects(server.handle('configuration/default-model', { revision: 0, provider: 'test', model: 'stale' }), { kind: 'settings/conflict' })
  await server.handle('configuration/get', {})
  const created = await server.handle('session/create', { cwd: process.cwd(), useDefaultModel: true })
  assert.equal(created.session.model, 'next')
  const overridden = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'explicit' })
  assert.equal(overridden.session.model, 'explicit')
  await assert.rejects(server.handle('session/create', { cwd: process.cwd(), useDefaultModel: true, provider: 'test', model: 'bad' }), { code: -32602 })
  await server.shutdown()
})

test('credentials use the configured provider reference, never resolve or return a secret', async () => {
  const { server, ctx, references } = runtime()
  ctx.credentials.resolve = () => { throw new Error('must not resolve') }
  const saved = await server.handle('configuration/credential', { reference: 'MYGO_TEST_KEY', operation: 'set', value: 'fake-test-only-key' })
  assert.equal(saved.credential.configured, true)
  assert.ok(!JSON.stringify(saved).includes('fake-test-only-key'))
  await assert.rejects(server.handle('configuration/credential', { reference: 'OTHER_KEY', operation: 'unset' }), { kind: 'settings/conflict' })
  await server.handle('configuration/get', {})
  assert.equal((await server.handle('configuration/credential', { reference: 'MYGO_TEST_KEY', operation: 'unset' })).credential.configured, false)
  assert.ok(references.every((reference) => reference === 'MYGO_TEST_KEY'))
  for (const value of ['', null, 'x\n', 'x'.repeat(8193)]) await assert.rejects(server.handle('configuration/credential', { reference: 'MYGO_TEST_KEY', operation: 'set', value }), { code: -32602 })
})

test('configuration errors are redacted and failed writes require readback', async () => {
  const { server, ctx } = runtime()
  ctx.credentials.set = async () => { throw Object.assign(new Error('never-forward'), { kind: 'unsafe-kind' }) }
  await assert.rejects(server.handle('configuration/credential', { reference: 'MYGO_TEST_KEY', operation: 'set', value: 'fake-only' }), (error) => error.kind === 'settings/write-failed' && !error.message.includes('never-forward'))
  await assert.rejects(server.handle('configuration/default-model', { revision: 0, provider: 'test', model: 'next' }), { kind: 'settings/refresh-required' })
  await server.handle('configuration/get', {})
  await server.handle('configuration/default-model', { revision: 0, provider: 'test', model: 'next' })
})

test('unsettled writes cannot be overtaken by reads or another mutation', async () => {
  const { server, ctx } = runtime()
  let finish
  const store = ctx.credentials.set
  ctx.credentials.set = async (...args) => {
    await new Promise((resolve) => { finish = resolve })
    await store(...args)
  }
  const saving = server.handle('configuration/credential', { reference: 'MYGO_TEST_KEY', operation: 'set', value: 'fake-only' })
  await new Promise((resolve) => setImmediate(resolve))
  assert.equal(typeof finish, 'function')
  await assert.rejects(server.handle('configuration/default-model', { revision: 0, provider: 'test', model: 'next' }), { kind: 'settings/busy' })
  let read = false
  const reading = server.handle('configuration/get', {}).then(() => { read = true })
  await Promise.resolve()
  assert.equal(read, false)
  finish()
  await Promise.all([saving, reading])
  assert.equal(server.configuration.uncertain, false)
  await server.handle('configuration/default-model', { revision: 0, provider: 'test', model: 'next' })
})

test('credential status sanitizes sources and clearing does not claim ambient revocation', async () => {
  const { server, ctx } = runtime()
  ctx.credentials.describe = async () => ({ configured: true, writable: true, source: 'never-forward' })
  const cleared = await server.handle('configuration/credential', { reference: 'MYGO_TEST_KEY', operation: 'unset' })
  assert.equal(cleared.credential.configured, true)
  assert.equal(cleared.credential.source, 'fallback')
  assert.ok(!JSON.stringify(cleared).includes('never-forward'))
  ctx.llm.listConfigurableProviders = () => []
  assert.equal((await server.handle('configuration/get', {})).credential.available, false)
  await assert.rejects(server.handle('configuration/credential', { reference: 'MYGO_TEST_KEY', operation: 'set', value: 'fake-only' }), { kind: 'settings/conflict' })
})

test('busy sessions block global writes; unavailable services disable capability', async () => {
  const { server, ctx } = runtime()
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'echo' })
  server.sessions.get(session.id).busy = true
  await assert.rejects(server.handle('configuration/default-model', { revision: 0, provider: 'test', model: 'next' }), { kind: 'session/busy' })
  server.sessions.get(session.id).busy = false
  ctx.credentials.set = async () => {}
  await assert.rejects(server.handle('configuration/credential', { reference: 'MYGO_TEST_KEY', operation: 'set', value: 'fake-only' }), { kind: 'settings/write-failed' })
  ctx.settings = undefined
  assert.equal(server.configuration.supported(), false)
  await server.shutdown()
})
