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
        session: { id: sessionId, seq: 0 }, status: 'idle', turns: 0,
        followup(message) {
          const work = { ...Promise.withResolvers(), finished: false, cancelled: false }
          agent.work = work
          const turn = ++agent.turns
          setImmediate(() => {
            agent.status = 'running'
            events.emit('agent/status', { agent, status: 'running' })
            const event = (type, data) => events.emit('session/event', agent.session, { type, data, seq: agent.session.seq++, time: Date.now() })
            event('turn/start', { turn: 1 })
            event('user/message', message)
            work.finish = (aborted) => {
              if (work.finished) return
              work.finished = true
              if (!aborted) {
                const callId = `fake-${turn}`
                event('tool/call', { callId, name: 'read_file', arguments: '{"path":"README.md"}' })
                event('tool/result', { message: { toolCallId: callId, content: [{ type: 'text', text: '# dsh-mygo' }] } })
                event('assistant/message', { message: { content: [{ type: 'text', text: `Echo: ${message.content.map((block) => block.text).join('\n')}` }] } })
              }
              event('turn/end', { reason: aborted ? { kind: 'aborted', reason: { kind: 'user' } } : { kind: 'completed' } })
              agent.status = 'idle'
              agent.work = undefined
              events.emit('agent/status', { agent, status: 'idle' })
              work.resolve()
            }
            if (work.cancelled) work.finish(true)
            else if (!process.argv.includes('--wait-for-cancel') || turn > 1) work.finish(false)
          })
        },
        cancel() {
          if (!agent.work) return
          agent.work.cancelled = true
          agent.work.finish?.(true)
        },
        whenIdle() { return agent.work?.promise ?? Promise.resolve() },
      }
      return { agent, async dispose() { agent.cancel(); await agent.whenIdle() } }
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
  if (method === 'initialize' && process.argv.includes('--no-cancel')) { result.capabilities = []; server.capabilities = [] }
  if (method === 'session/create' && process.argv.includes('--bad-session')) result.session.id = '../unsafe'
  if (method === 'shutdown') setImmediate(async () => { await channel.flush(); process.exit(0) })
  return result
}, async (error) => { await server.shutdown(); process.exit(error ? 1 : 0) })
