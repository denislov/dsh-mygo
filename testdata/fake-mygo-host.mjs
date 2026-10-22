import { EventEmitter } from 'node:events'
import { randomUUID } from 'node:crypto'
import { LineChannel } from '../resources/host/transport.mjs'
import { MygoServer } from '../resources/host/server.mjs'

const events = new EventEmitter()
if (process.argv.includes('--stderr-secret')) process.stderr.write('fake-private-token\n')
const ctx = {
  get(name) { return this[name] },
  on(name, handler) { events.on(name, handler); return () => events.off(name, handler) },
  llm: { async resolveCallConfig() {} },
  agents: {
    async create({ sessionId }) {
      const agent = {
        session: { id: sessionId, seq: 0 }, status: 'idle',
        followup(message) {
          setImmediate(() => {
            agent.status = 'running'
            events.emit('agent/status', { agent, status: 'running' })
            const event = (type, data) => events.emit('session/event', agent.session, { type, data, seq: agent.session.seq++, time: Date.now() })
            event('turn/start', { turn: 1 })
            event('user/message', message)
            event('tool/call', { callId: 'fake-1', name: 'read_file', arguments: '{"path":"README.md"}' })
            event('tool/result', { message: { toolCallId: 'fake-1', content: [{ type: 'text', text: '# dsh-mygo' }] } })
            event('assistant/message', { message: { content: [{ type: 'text', text: `Echo: ${message.content.map((block) => block.text).join('\n')}` }] } })
            event('turn/end', { reason: { kind: 'completed' } })
            agent.status = 'idle'
            events.emit('agent/status', { agent, status: 'idle' })
          })
        },
      }
      return { agent, async dispose() {} }
    },
  },
}
const channel = new LineChannel(process.stdin, process.stdout, { maxFrameBytes: 16 * 1024 * 1024 })
const server = new MygoServer(ctx, {
  notify: (method, params) => channel.notify(method, params),
  createUserMessage: (options) => ({ ...options, id: randomUUID() }),
  reasoningEffort: (value) => value, dshVersion: '0.2.1-rc.2',
})
channel.start(async (method, params) => {
  const result = await server.handle(method, params)
  if (method === 'initialize' && process.argv.includes('--bad-version')) result.protocolVersion = 2
  if (method === 'session/create' && process.argv.includes('--bad-session')) result.session.id = '../unsafe'
  if (method === 'shutdown') setImmediate(async () => { await channel.flush(); process.exit(0) })
  return result
}, async (error) => { await server.shutdown(); process.exit(error ? 1 : 0) })
