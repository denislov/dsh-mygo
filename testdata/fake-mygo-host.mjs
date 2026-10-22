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
        session: { id: sessionId, seq: 0 }, status: 'idle', turns: 0, revision: 0,
        followup(message) {
          const work = { ...Promise.withResolvers(), finished: false, cancelled: false, abort: new AbortController() }
          agent.work = work
          const turn = ++agent.turns
          setImmediate(() => {
            agent.status = 'running'
            events.emit('agent/status', { agent, status: 'running' })
            const event = (type, data) => {
              const seq = agent.session.seq++
              events.emit('session/event', agent.session, { type, data, seq, time: Date.now() })
              return seq
            }
            const stream = (frame) => events.emit('agent/assistant-stream', { agent, frame: { attemptId: `${sessionId}:${turn}`, revision: ++agent.revision, ...frame } })
            let streamIndex = 0
            const chunk = (chunk) => stream({ type: 'chunk', index: streamIndex++, time: Date.now(), chunk })
            event('turn/start', { turn })
            event('user/message', message)
            work.finish = (aborted, approvalOutcome) => {
              if (work.finished) return
              work.finished = true
              clearTimeout(work.timer)
              if (!aborted) {
                const callId = `fake-${turn}`
                const diffFixture = process.argv.includes('--tool-diff') && turn === 1
                if (!work.approval && !work.question && !work.streaming) event('tool/call', diffFixture
                  ? { callId, name: 'edit', arguments: '{"file_path":"demo.txt","old_string":"before","new_string":"after"}' }
                  : { callId, name: 'read_file', arguments: '{"path":"README.md"}' })
                const reply = work.streaming ? 'Live prefix complete' : work.question ? approvalOutcome : work.approval ? approvalOutcome === 'allowed-once' ? 'Approved write' : 'Denied write' : `Echo: ${message.content.map((block) => block.text).join('\n')}`
                if (work.streaming) {
                  chunk({ type: 'text-delta', index: 0, text: ' complete' })
                  chunk({ type: 'block-end', index: 0, block: { type: 'text', text: reply } })
                } else event('tool/result', { message: { toolCallId: callId, content: [{ type: 'text', text: diffFixture ? 'Updated demo.txt (fixture only)' : work.approval ? reply : '# dsh-mygo' }] },
                  ...(diffFixture ? { meta: { operation: 'update', diffs: [
                    { path: 'demo.txt', oldText: 'before', newText: 'after' },
                    { path: 'second.txt', oldText: null, newText: 'created' },
                  ] } } : {}),
                })
                const seq = event('assistant/message', { turn, step: 1, message: { content: [{ type: 'text', text: reply }] } })
                if (work.streaming) stream({ type: 'end', index: streamIndex, outcome: { kind: 'committed', eventType: 'assistant/message', seq } })
              } else if (work.streaming) {
                const seq = event('assistant/message', { turn, step: 1, interrupted: true, message: { content: [{ type: 'text', text: 'Live prefix' }] } })
                stream({ type: 'end', index: streamIndex, outcome: { kind: 'committed', eventType: 'assistant/message', seq } })
              }
              event('turn/end', { reason: aborted ? { kind: 'aborted', reason: { kind: 'user' } } : { kind: 'completed' } })
              agent.status = 'idle'
              agent.work = undefined
              events.emit('agent/status', { agent, status: 'idle' })
              work.resolve()
            }
            if (work.cancelled) work.finish(true)
            else if ((process.argv.includes('--stream') || process.argv.includes('--stream-wait')) && turn === 1) {
              work.streaming = true
              stream({ type: 'start', turn, step: 1 })
              chunk({ type: 'block-start', index: 0, blockType: 'text' })
              chunk({ type: 'text-delta', index: 0, text: 'Live prefix' })
              if (!process.argv.includes('--stream-wait')) work.timer = setTimeout(() => work.finish(false), 700)
            }
            else if ((process.argv.includes('--question') && turn === 1) || (process.argv.includes('--question-twice') && turn <= 2)) {
              work.question = true
              const questions = [
                { id: 'mode', question: 'Choose mode', detail: 'Choose how the next task should run.', options: [{ label: 'Safe', description: 'Recommended mode' }, { label: 'Fast' }] },
                { id: 'features', question: 'Choose features', multiSelect: true, options: [{ label: 'Logs' }, { label: 'Tests' }] },
                { id: 'note', question: 'Add a note' },
              ]
              event('tool/call', { callId: `fake-${turn}`, name: 'ask_user_question', arguments: JSON.stringify({ questions }) })
              void server.requestQuestion({ agent, questions, signal: work.abort.signal })
                .then((answer) => work.finish(false, `Question answered: ${JSON.stringify(answer)}`), () => work.finish(false, 'Question cancelled'))
            }
            else if (process.argv.includes('--approval') && turn === 1) {
              work.approval = true
              const callId = `fake-${turn}`
              event('tool/call', { callId, name: 'write', arguments: '{"file_path":"approval-marker.txt","content":"hello"}' })
              void server.requestApproval({ agent, toolName: 'write', callId, reason: 'Write a file in the temporary workspace', signal: work.abort.signal })
                .then((outcome) => work.finish(false, outcome))
            }
            else if (!process.argv.includes('--wait-for-cancel') || turn > 1) work.finish(false)
          })
        },
        cancel() {
          if (!agent.work) return
          agent.work.cancelled = true
          agent.work.abort.abort()
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
  if (method === 'initialize' && process.argv.includes('--no-stream')) {
    server.capabilities = result.capabilities = result.capabilities.filter((capability) => capability !== 'session.assistant-stream')
  }
  if (method === 'session/create' && process.argv.includes('--bad-session')) result.session.id = '../unsafe'
  if (method === 'shutdown') setImmediate(async () => { await channel.flush(); process.exit(0) })
  return result
}, async (error) => { await server.shutdown(); process.exit(error ? 1 : 0) })
