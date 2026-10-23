import { EventEmitter } from 'node:events'
import { randomUUID } from 'node:crypto'
import { LineChannel } from '../resources/host/transport.mjs'
import { MygoServer } from '../resources/host/server.mjs'
import { markdownPrefix, markdownReply } from './markdown-sample.mjs'

const events = new EventEmitter()
const stored = new Map()
events.on('session/event', (session, event) => stored.get(session.id)?.events.push(event))
if (process.argv.includes('--stderr-secret')) process.stderr.write('fake-private-token\n')
const ctx = {
  get(name) { return this[name] },
  on(name, handler) { events.on(name, handler); return () => events.off(name, handler) },
  llm: { async resolveCallConfig() {} },
  agents: {
    async create({ sessionId, meta, setup, agentOptions }) {
      const source = stored.get(sessionId) ?? { header: { id: sessionId, cwd: meta?.cwd ?? process.cwd(), createdAt: Date.now(), version: 1, isSeeded: false }, events: [] }
      stored.set(sessionId, source)
      const agentScope = {}
      const agent = {
        session: { id: sessionId, header: source.header, seq: source.events.length, append(type, data) { events.emit('session/event', this, { type, data, seq: this.seq++, time: Date.now() }) } }, status: 'idle', turns: source.events.filter((event) => event.type === 'turn/start').length, revision: 0,
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
                let reply = work.streaming ? work.markdown ? markdownReply : 'Live prefix complete' : work.question ? approvalOutcome : work.approval ? approvalOutcome === 'allowed-once' ? 'Approved write' : 'Denied write' : `Echo: ${message.content.map((block) => block.text).join('\n')}`
                const selected = agentScope.selection?.current ?? agentOptions
                if (selected?.model === 'route-probe') reply = `Selected ${selected.provider}/${selected.model}/${selected.reasoningEffort ?? 'default'}`
                if (work.streaming) {
                  chunk({ type: 'text-delta', index: 0, text: work.markdown ? markdownReply.slice(markdownPrefix.length) : ' complete' })
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
                const seq = event('assistant/message', { turn, step: 1, interrupted: true, message: { content: [{ type: 'text', text: work.markdown ? markdownPrefix : 'Live prefix' }] } })
                stream({ type: 'end', index: streamIndex, outcome: { kind: 'committed', eventType: 'assistant/message', seq } })
              }
              event('turn/end', { reason: aborted ? { kind: 'aborted', reason: { kind: 'user' } } : { kind: 'completed' } })
              agent.status = 'idle'
              agent.work = undefined
              events.emit('agent/status', { agent, status: 'idle' })
              work.resolve()
            }
            if (work.cancelled) work.finish(true)
            else if (['--stream', '--stream-wait', '--markdown', '--markdown-wait'].some((flag) => process.argv.includes(flag)) && turn === 1) {
              work.streaming = true
              work.markdown = process.argv.includes('--markdown') || process.argv.includes('--markdown-wait')
              stream({ type: 'start', turn, step: 1 })
              chunk({ type: 'block-start', index: 0, blockType: 'text' })
              chunk({ type: 'text-delta', index: 0, text: work.markdown ? markdownPrefix : 'Live prefix' })
              if (!process.argv.includes('--stream-wait') && !process.argv.includes('--markdown-wait')) work.timer = setTimeout(() => work.finish(false), 700)
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
      setup?.(agentScope, agent)
      return { agent, async dispose() { agent.cancel(); await agent.whenIdle() } }
    },
  },
}
if (process.argv.includes('--sessions')) {
  const id = 'demo-history'
  const time = Date.now() - 60000
  stored.set(id, { header: { id, cwd: process.cwd(), createdAt: time, version: 1, isSeeded: false }, events: [
    { seq: 0, time, type: 'turn/start', data: { turn: 1 } },
    { seq: 1, time, type: 'user/message', data: { source: { kind: 'user' }, content: [{ type: 'text', text: 'Saved prompt' }] } },
    { seq: 2, time, type: 'assistant/message', data: { message: { content: [{ type: 'text', text: 'Saved conversation' }] } } },
    { seq: 3, time, type: 'turn/end', data: { reason: { kind: 'completed' } } },
  ] })
  ctx.agents.resume = async ({ resumeSessionId, setup }) => {
    if (!stored.has(resumeSessionId)) throw new Error('Unknown fixture session')
    return ctx.agents.create({ sessionId: resumeSessionId, setup })
  }
  ctx.sessionPersistence = {
    async list() { return [...stored.values()].map(({ header }) => ({ header })) },
    async stat(id) { return stored.get(id) },
    async flush() {},
    async open(id) {
      return { async read(offset, length) { return { events: stored.get(id).events.slice(offset, offset + length) } }, async close() {} }
    },
  }
}
if (process.argv.includes('--session-management')) {
  const archived = new Set()
  ctx.sessionTitle = {
    rename(session, title) { session.append('session/title', { title: title.trim(), messageSeqs: [], source: { kind: 'user' } }) },
  }
  ctx.workspaceRegistry = {
    get archivedSessionIds() { return [...archived] },
    async archiveSession(id) { archived.add(id) },
    async unarchiveSession(id) { archived.delete(id) },
  }
}
if (process.argv.includes('--models')) {
  ctx.llm.listProviders = () => [{ id: 'test', name: 'Test provider' }]
  ctx.llm.listModels = async (provider) => [{ provider, id: 'echo', name: 'Echo' }, { provider, id: 'route-probe', name: 'Route probe' }]
  ctx.llm.resolveModelInfo = async (_provider, model) => model === 'route-probe' ? { reasoning: { efforts: [{ id: 'low', name: 'Low' }, { id: 'high', name: 'High' }] } } : {}
  ctx.llm.resolveCallConfig = async ({ provider, model, reasoningEffort }) => {
    if (provider !== 'test' || model === 'bad' || reasoningEffort !== undefined && (model !== 'route-probe' || !['low', 'high'].includes(reasoningEffort))) throw new Error('Invalid fixture model route')
  }
}
const channel = new LineChannel(process.stdin, process.stdout, { maxFrameBytes: 16 * 1024 * 1024 })
if (process.argv.includes('--configuration')) {
  let defaults = { provider: 'test', model: 'echo', reasoningEffort: '' }
  let revision = 0
  let credential
  ctx.agentDefaultModel = { currentSelection: () => ({ ...defaults }) }
  ctx.settings = {
    describe: () => [{ ns: 'agent-default-model', revision, value: { ...defaults } }, { ns: 'llm-deepseek', revision: 0, value: { apiKeyEnv: 'MYGO_TEST_ONLY_KEY' } }],
    async replace(_ns, value, expected) {
      if (revision !== expected) throw Object.assign(new Error('Conflict'), { code: 'SETTINGS_CONFLICT' })
      defaults = { ...value }; revision++
    },
  }
  ctx.llm.listConfigurableProviders = () => [{ provider: 'deepseek-official', settingsNs: 'llm-deepseek', settingsPath: [] }]
  ctx.credentials = {
    async describe() { return { configured: credential !== undefined, writable: true, ...(credential === undefined ? {} : { source: 'file' }) } },
    async set(_ref, value) { credential = value },
    async unset() { credential = undefined },
  }
}
const server = new MygoServer(ctx, {
  notify: (method, params) => {
    if (process.argv.includes('--stream-gap') && method === 'session/assistant-stream' && params.frame.chunk?.type === 'block-start' && params.frame.chunk.blockType === 'text' && !server.streamGapInjected) {
      server.streamGapInjected = true
      return
    }
    if (process.argv.includes('--event-gap') && method === 'session/event' && params.event.type === 'user/message' && !server.gapInjected) {
      server.gapInjected = true
      return
    }
    channel.notify(method, params)
  },
  createUserMessage: (options) => ({ ...options, id: randomUUID() }),
  reasoningEffort: (value) => value, dshVersion: '0.2.1-rc.2',
  ...(process.argv.includes('--configuration') ? { credentialRef: (value) => value } : {}),
  ...(process.argv.includes('--models') ? { installModelSelection: (agentScope, selection) => { agentScope.selection = selection } } : {}),
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
