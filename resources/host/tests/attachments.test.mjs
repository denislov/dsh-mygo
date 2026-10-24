import assert from 'node:assert/strict'
import { test } from 'node:test'
import { MygoServer } from '../server.mjs'
import { maxBase64Bytes } from '../attachments.mjs'

const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFAAH/q842iQAAAABJRU5ErkJggg=='
const handshake = {
  protocolVersion: 1,
  client: { name: 'dsh-mygo', version: '0.1.1', platform: 'test', locale: 'en-US' },
  capabilities: ['attachments'],
}

function imageBlock(overrides = {}) {
  return { type: 'image', mimeType: 'image/png', data: PNG, ...overrides }
}

function encoded(size) {
  return Buffer.alloc(size, 7).toString('base64')
}

function attachmentStore({ fail = false, limits = {} } = {}) {
  const saved = []
  return {
    saved,
    imageLimits: {
      mediaTypes: ['image/png', 'image/jpeg', 'image/webp', 'image/gif', 'image/bmp'],
      maxImageBytes: 20 * 1024 * 1024,
      maxImagesPerMessage: 20,
      maxMessageImageBytes: 200 * 1024 * 1024,
      maxImagePixels: 64000000,
      maxImageDimension: 8192,
      ...limits,
    },
    async admitPromptContent(parts) {
      for (const part of parts) {
        if (part.type !== 'image') continue
        if (fail) throw Object.assign(new Error('Image batch exceeds the configured aggregate image-byte limit.'), { code: 'IMAGES_TOO_LARGE' })
        saved.push(part)
      }
      return parts.map((part) => part.type === 'image'
        ? { type: 'image', attachment: { attachmentId: 'sha256:0f', mediaType: part.mediaType, bytes: Buffer.from(part.data, 'base64').length, width: 1, height: 1, ...(part.name === undefined ? {} : { name: part.name }) } }
        : part)
    },
  }
}

async function runtime({ capabilities = handshake.capabilities, attachments, modalities = ['text', 'image'], routeError } = {}) {
  const listeners = new Map()
  const messages = []
  const ctx = {
    get(name) { return this[name] },
    on(name, listener) { listeners.set(name, listener); return () => listeners.delete(name) },
    llm: {
      listProviders() { return [{ id: 'test', name: 'Test' }] },
      async listModels() { return [{ id: 'vision', name: 'Vision', provider: 'test' }] },
      async resolveModelInfo() {
        if (routeError) throw routeError
        return { reasoning: { efforts: [] }, ...(modalities === undefined ? {} : { inputModalities: modalities }) }
      },
      async resolveCallConfig() {},
    },
    agents: {
      async create(options) {
        const session = { id: options.sessionId, seq: 0, header: { id: options.sessionId, createdAt: 1, cwd: process.cwd() }, append(type, data) { listeners.get('session/event')?.(session, { seq: this.seq++, time: 1, type, data }) } }
        return {
          agent: {
            session,
            status: 'idle',
            followup(message) { messages.push(message) },
          },
          async dispose() {},
        }
      },
    },
    attachments,
  }
  const server = new MygoServer(ctx, {
    notify: () => {},
    createUserMessage: (value) => ({ id: 'm1', ...value }),
    reasoningEffort: (value) => value,
    dshVersion: '0.2.1-rc.2',
  })
  const initialized = await server.handle('initialize', { ...handshake, capabilities })
  const { session } = await server.handle('session/create', { cwd: process.cwd(), provider: 'test', model: 'vision' })
  return { ctx, server, session, initialized, messages }
}

test('attachment capability is advertised only with a store and model seam', async () => {
  const bare = await runtime({ attachments: undefined })
  assert.deepEqual(bare.initialized.capabilities, [])
  assert.equal(bare.initialized.attachments, undefined)
  await assert.rejects(bare.server.handle('session/prompt', { sessionId: bare.session.id, content: [imageBlock()] }), { kind: 'session/attachment-invalid' })
  await bare.server.shutdown()

  const store = attachmentStore()
  const { initialized, server, session } = await runtime({ attachments: store })
  assert.deepEqual(initialized.capabilities, ['attachments'])
  assert.deepEqual(initialized.attachments.image.mediaTypes, ['image/png', 'image/jpeg', 'image/webp', 'image/gif'])
  assert.equal(initialized.attachments.image.maxBase64Bytes, maxBase64Bytes)
  assert.equal(initialized.attachments.image.maxImageBytes, 6 * 1024 * 1024)
  assert.equal(initialized.attachments.image.maxImagesPerMessage, 8)
  await server.shutdown()
  assert.equal(store.saved.length, 0)
})

test('the advertised policy tightens itself to the deployment limits', async () => {
  const store = attachmentStore({ limits: { mediaTypes: ['image/gif'], maxImageBytes: 1024, maxImagesPerMessage: 2, maxMessageImageBytes: 2048 } })
  const { initialized, server } = await runtime({ attachments: store })
  assert.deepEqual(initialized.attachments.image.mediaTypes, ['image/gif'])
  assert.equal(initialized.attachments.image.maxImageBytes, 1024)
  assert.equal(initialized.attachments.image.maxImagesPerMessage, 2)
  assert.equal(initialized.attachments.image.maxMessageImageBytes, 2048)
  await server.shutdown()

  const unsupported = attachmentStore({ limits: { mediaTypes: ['image/bmp'] } })
  const disabled = await runtime({ attachments: unsupported })
  assert.deepEqual(disabled.initialized.capabilities, [])
  await disabled.server.shutdown()
})

test('image blocks are validated before any storage write', async () => {
  const store = attachmentStore()
  const { server, session } = await runtime({ attachments: store })
  const invalid = [
    [imageBlock({ mimeType: 'image/bmp' })],
    [imageBlock({ mimeType: 'image/svg+xml' })],
    [imageBlock({ data: PNG.replace(/=+$/, '') })],
    [imageBlock({ data: ` ${PNG}` })],
    [imageBlock({ data: encoded(7 * 1024 * 1024) })],
    [imageBlock({ name: '../secret.png' })],
    [imageBlock({ name: 'a/b.png' })],
    [{ type: 'text', text: '' }],
    [{ type: 'audio', data: PNG }],
    [],
    Array.from({ length: 65 }, () => ({ type: 'text', text: 'x' })),
  ]
  for (const content of invalid) await assert.rejects(server.handle('session/prompt', { sessionId: session.id, content }), (error) => error.code === -32602, JSON.stringify(content).slice(0, 80))
  await assert.rejects(server.handle('session/prompt', { sessionId: session.id, content: [imageBlock({ data: encoded(6 * 1024 * 1024 + 1) })] }), { kind: 'protocol/invalid-params' })
  assert.equal(store.saved.length, 0)
  await server.shutdown()
})

test('image count and aggregate budgets are enforced', async () => {
  const store = attachmentStore()
  const { server, session } = await runtime({ attachments: store })
  const tooMany = Array.from({ length: 9 }, () => imageBlock())
  await assert.rejects(server.handle('session/prompt', { sessionId: session.id, content: tooMany }), { kind: 'session/attachment-invalid' })
  const chunk = encoded(3 * 1024 * 1024)
  const large = encoded(4 * 1024 * 1024)
  await assert.rejects(server.handle('session/prompt', { sessionId: session.id, content: [imageBlock({ data: chunk }), imageBlock({ data: large })] }), { kind: 'session/attachment-invalid' })
  const stored = await server.handle('session/prompt', { sessionId: session.id, content: [imageBlock()] })
  assert.equal(stored.messageId, 'm1')
  assert.equal(store.saved.length, 1)
  await server.shutdown()
})

test('the current model route must declare image input', async () => {
  const store = attachmentStore()
  const textOnly = await runtime({ attachments: store, modalities: ['text'] })
  await assert.rejects(textOnly.server.handle('session/prompt', { sessionId: textOnly.session.id, content: [imageBlock()] }), { kind: 'session/attachment-invalid' })
  assert.equal(store.saved.length, 0)
  assert.equal(textOnly.messages.length, 0)
  await textOnly.server.shutdown()

  const unknown = await runtime({ attachments: store, modalities: undefined })
  await unknown.server.handle('session/prompt', { sessionId: unknown.session.id, content: [imageBlock()] })
  assert.equal(unknown.messages.length, 1)
  await unknown.server.shutdown()

  const broken = await runtime({ attachments: store, routeError: new Error('private-token') })
  await assert.rejects(broken.server.handle('session/prompt', { sessionId: broken.session.id, content: [imageBlock()] }), (error) => error.kind === 'session/attachment-invalid' && !error.message.includes('private-token'))
  await broken.server.shutdown()
})

test('admission keeps wire order and stores durable references only', async () => {
  const store = attachmentStore()
  const { server, session, messages } = await runtime({ attachments: store })
  await server.handle('session/prompt', {
    sessionId: session.id,
    content: [{ type: 'text', text: 'look' }, imageBlock({ name: 'shot.png' }), { type: 'text', text: 'thanks' }],
  })
  assert.equal(messages.length, 1)
  const [text, image, tail] = messages[0].content
  assert.deepEqual(text, { type: 'text', text: 'look' })
  assert.equal(image.type, 'image')
  assert.equal(image.attachment.attachmentId, 'sha256:0f')
  assert.equal(image.attachment.mediaType, 'image/png')
  assert.equal(image.attachment.name, 'shot.png')
  assert.deepEqual(tail, { type: 'text', text: 'thanks' })
  assert.equal(store.saved.length, 1)
  assert.ok(!JSON.stringify(messages[0]).includes(PNG))
  await server.shutdown()
})

test('a refused batch resets the session and never leaks payloads', async () => {
  const store = attachmentStore({ fail: true })
  const { server, session, messages } = await runtime({ attachments: store })
  await assert.rejects(server.handle('session/prompt', { sessionId: session.id, content: [imageBlock()] }), (error) => error.kind === 'session/attachment-invalid' && !error.message.includes(PNG))
  assert.equal(messages.length, 0)
  assert.equal(server.sessions.get(session.id).busy, false)
  await server.shutdown()
})

test('text prompts keep working without the capability', async () => {
  const { server, session, messages } = await runtime({ capabilities: [], attachments: attachmentStore() })
  await server.handle('session/prompt', { sessionId: session.id, content: [{ type: 'text', text: 'hello' }] })
  assert.deepEqual(messages[0].content, [{ type: 'text', text: 'hello' }])
  await server.shutdown()
})
