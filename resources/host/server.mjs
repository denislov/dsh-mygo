import { randomUUID } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { stat } from 'node:fs/promises'
import { isAbsolute } from 'node:path'
import { ApprovalQueue } from './approval.mjs'
import { QuestionQueue } from './question.mjs'
import { SessionHistory, sessionID, sessionSummary } from './history.mjs'
import { ModelSettings } from './models.mjs'
import { Configuration } from './configuration.mjs'
import { SessionManagement } from './sessions.mjs'

const identity = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8'))
if (!Number.isSafeInteger(identity.dshMygo?.protocolVersion)) {
  throw new Error('The host package must declare dshMygo.protocolVersion')
}
export const protocolVersion = identity.dshMygo.protocolVersion
export const releaseVersion = identity.version
export const supportedDshVersion = identity.engines.dsh

function fault(kind, message, code = -32000) {
  return Object.assign(new Error(message), { kind, code })
}

function text(value, field, limit = 256) {
  if (typeof value !== 'string' || value.trim().length === 0 || value.length > limit || value.includes('\0')) {
    throw fault('protocol/invalid-params', `Invalid ${field}`, -32602)
  }
  return value
}

export class MygoServer {
  constructor(ctx, { notify, createUserMessage, reasoningEffort, ensureProvider, installModelSelection, credentialRef, dshVersion, maxSessions = 8, cancelTimeoutMs = 5000, maxPendingApprovals = 32, maxPendingQuestions = 32, maxFrameBytes = 16 * 1024 * 1024 }) {
    this.ctx = ctx
    this.notify = notify
    this.createUserMessage = createUserMessage
    this.reasoningEffort = reasoningEffort
    this.ensureProvider = ensureProvider
    this.installModelSelection = installModelSelection
    this.dshVersion = dshVersion
    this.maxSessions = maxSessions
    this.cancelTimeoutMs = cancelTimeoutMs
    this.capabilities = []
    this.approvals = new ApprovalQueue({ notify, maxPending: maxPendingApprovals })
    this.questions = new QuestionQueue({ notify, maxPending: maxPendingQuestions })
    this.sessions = new Map()
    this.history = new SessionHistory(ctx, this.sessions, Math.floor(maxFrameBytes / 2))
    this.models = new ModelSettings(this, Math.floor(maxFrameBytes / 2))
    this.configuration = new Configuration(this, credentialRef)
    this.management = new SessionManagement(this)
    this.initialized = false
    this.stopping = false
    this.disposers = [
      ctx.on('agent/assistant-stream', ({ agent, frame }) => {
        if (this.stopping || !this.capabilities.includes('session.assistant-stream')) return
        const sessionId = String(agent.session.id)
        if (this.sessions.get(sessionId)?.handle?.agent !== agent) return
        this.notify('session/assistant-stream', { sessionId, frame })
      }),
      ctx.on('session/event', (session, event) => {
        const record = this.sessions.get(String(session.id))
        if (record?.handle?.agent.session !== session) return
        record.metadata = {
          ...record.metadata,
          updatedAt: Math.max(record.metadata?.updatedAt ?? session.header.createdAt, event.time),
          ...(event.type === 'session/title' ? { title: event.data.title } : {}),
        }
        if (event.type === 'tool/call') {
          if (typeof event.data.callId === 'string' && record.calls.size < 256) record.calls.set(event.data.callId, { name: event.data.name, arguments: event.data.arguments })
        } else if (event.type === 'tool/result') {
          record.calls.delete(event.data.message?.toolCallId)
        } else if (event.type === 'turn/end') {
          record.calls.clear()
          this.approvals.cancelSession(String(session.id))
          this.questions.cancelSession(String(session.id))
        }
        this.notify('session/event', { sessionId: String(session.id), seq: event.seq, event })
      }),
      ctx.on('agent/status', ({ agent, status }) => {
        const record = this.sessions.get(String(agent.session.id))
        if (record?.handle?.agent !== agent) return
        if (status === 'idle') {
          record.busy = false
          this.approvals.cancelSession(String(agent.session.id))
          this.questions.cancelSession(String(agent.session.id))
        }
        this.notifyStatus(record, String(agent.session.id), status)
      }),
      ctx.on('agent/error', ({ agent }) => {
        if (this.sessions.get(String(agent.session.id))?.handle?.agent !== agent) return
        this.approvals.cancelSession(String(agent.session.id))
        this.questions.cancelSession(String(agent.session.id))
        this.notify('host/fatal', { message: 'The agent failed. Restart the host before retrying.' })
      }),
    ]
  }

  async handle(method, params) {
    if (method === 'shutdown') return this.shutdown()
    if (this.stopping) throw fault('host/stopping', 'Host is stopping')
    if (method === 'initialize') return this.initialize(params)
    if (!this.initialized) throw fault('protocol/not-initialized', 'Initialize the host first')
    if (this.configuration.pending && ['session/create', 'session/resume', 'session/prompt', 'session/rename', 'session/archive', 'model/select'].includes(method)) throw fault('settings/busy', 'A configuration write is still pending')
    switch (method) {
      case 'session/rename':
      case 'session/archive': {
        const operation = method.slice('session/'.length)
        if (!this.capabilities.includes(`session.${operation}`)) throw fault('protocol/capability-unavailable', 'Session management was not negotiated')
        return this.management[operation](params)
      }
      case 'configuration/get':
      case 'configuration/default-model':
      case 'configuration/credential':
        if (!this.capabilities.includes('settings.configuration')) throw fault('protocol/capability-unavailable', 'Configuration was not negotiated')
        if (method === 'configuration/get') return this.configuration.get()
        return method === 'configuration/default-model' ? this.configuration.saveDefaults(params) : this.configuration.saveCredential(params)
      case 'session/create': return this.create(params)
      case 'model/list':
      case 'model/select':
        if (!this.capabilities.includes('model.selection')) throw fault('protocol/capability-unavailable', 'Model selection was not negotiated')
        return method === 'model/list' ? this.models.list(params) : this.models.select(params)
      case 'session/list':
      case 'session/resume':
      case 'session/history':
        if (!this.capabilities.includes('session.history')) throw fault('protocol/capability-unavailable', 'Session history was not negotiated')
        if (method === 'session/history') {
          if (params.live !== undefined && typeof params.live !== 'boolean') throw fault('protocol/invalid-params', 'Invalid live history flag', -32602)
          if (params.live && !this.capabilities.includes('session.resync')) throw fault('protocol/capability-unavailable', 'Live history was not negotiated')
        }
        if (method === 'session/list') return this.history.list(params)
        return method === 'session/resume' ? this.resume(params) : this.history.history(params)
      case 'session/prompt': return this.prompt(params)
      case 'session/cancel': return this.cancel(params)
      case 'approval/respond':
        if (!this.capabilities.includes('approval')) throw fault('protocol/capability-unavailable', 'Approval was not negotiated')
        return this.approvals.respond(params)
      case 'question/respond':
      case 'question/cancel':
        if (!this.capabilities.includes('question')) throw fault('protocol/capability-unavailable', 'Questions were not negotiated')
        return method === 'question/respond' ? this.questions.respond(params) : this.questions.cancel(params)
      default: throw fault('protocol/method-not-found', 'Unknown channel method', -32601)
    }
  }

  initialize(params) {
    if (this.initialized) throw fault('protocol/already-initialized', 'Host is already initialized')
    if (!Number.isSafeInteger(params.protocolVersion)) throw fault('protocol/invalid-params', 'Invalid protocolVersion', -32602)
    if (params.protocolVersion !== protocolVersion) throw fault('protocol/version-mismatch', 'Incompatible channel protocol version')
    if (this.dshVersion !== supportedDshVersion) throw fault('protocol/runtime-mismatch', 'Incompatible DSH runtime version')
    if (params.client === null || typeof params.client !== 'object' || Array.isArray(params.client)) throw fault('protocol/invalid-params', 'Invalid client', -32602)
    for (const field of ['name', 'version', 'platform', 'locale']) text(params.client[field], `client.${field}`)
    if (params.client.version !== releaseVersion) throw fault('protocol/release-mismatch', 'UI and host release versions differ')
    if (!Array.isArray(params.capabilities) || params.capabilities.length > 64) throw fault('protocol/invalid-params', 'Invalid capabilities', -32602)
    for (const capability of params.capabilities) text(capability, 'capability')
    this.initialized = true
    this.capabilities = ['session.cancel', 'approval', 'question', 'session.assistant-stream'].filter((capability) => params.capabilities.includes(capability))
    const persistence = this.ctx.get('sessionPersistence')
    if (params.capabilities.includes('settings.configuration') && this.configuration.supported()) this.capabilities.push('settings.configuration')
    if (params.capabilities.includes('model.selection') && typeof this.installModelSelection === 'function' && ['listProviders', 'listModels', 'resolveModelInfo', 'resolveCallConfig'].every((method) => typeof this.ctx.llm[method] === 'function')) this.capabilities.push('model.selection')
    if (params.capabilities.includes('session.history') && ['list', 'stat', 'open', 'flush'].every((method) => typeof persistence?.[method] === 'function') && typeof this.ctx.agents.resume === 'function') {
      this.capabilities.push('session.history')
      if (params.capabilities.includes('session.resync')) this.capabilities.push('session.resync')
    }
    if (['stat', 'open', 'flush'].every((method) => typeof persistence?.[method] === 'function')) {
      if (params.capabilities.includes('session.rename') && typeof this.ctx.get('sessionTitle')?.rename === 'function') this.capabilities.push('session.rename')
      const registry = this.ctx.get('workspaceRegistry')
      if (params.capabilities.includes('session.archive') && typeof registry?.archiveSession === 'function' && typeof registry?.unarchiveSession === 'function') this.capabilities.push('session.archive')
    }
    return {
      server: { name: 'dsh-mygo-host', version: releaseVersion, dshVersion: this.dshVersion },
      protocolVersion, capabilities: this.capabilities,
    }
  }

  async create(params) {
    params = this.configuration.sessionRoute(params)
    const cwd = text(params.cwd, 'cwd', 32768)
    if (!isAbsolute(cwd)) throw fault('protocol/invalid-params', 'Session cwd must be absolute', -32602)
    const directory = await stat(cwd).catch(() => undefined)
    if (!directory?.isDirectory()) throw fault('protocol/invalid-params', 'Session cwd must be an existing directory', -32602)
    const provider = text(params.provider, 'provider')
    const model = text(params.model, 'model')
    const effort = params.effort === undefined ? undefined : text(params.effort, 'effort')
    if (this.sessions.size >= this.maxSessions) throw fault('session/limit', 'Host session limit reached')
    const record = { handle: undefined, busy: false, calls: new Map(), route: { provider, model, effort: effort ?? '' }, selection: { current: undefined, assembled: undefined } }
    const sessionId = randomUUID()
    this.sessions.set(sessionId, record)
    try {
      await this.ensureProvider?.(provider)
      const agentOptions = { provider, model, ...(effort === undefined ? {} : { reasoningEffort: this.reasoningEffort(effort) }) }
      await this.ctx.llm.resolveCallConfig(agentOptions)
      record.handle = await this.ctx.agents.create({ sessionId, meta: { cwd }, agentOptions, ...(this.installModelSelection ? { setup: (agentCtx) => { this.installModelSelection(agentCtx, record.selection) } } : {}) })
      if (this.ctx.get('sessionTitle')) {
        await this.ctx.get('sessionPersistence').flush()
        return { session: await this.history.describe(record.handle.agent.session.header, record) }
      }
      return { session: {
        id: sessionId, title: sessionId.slice(0, 8), cwd, provider, model, effort: effort ?? '',
        createdAt: Date.now(), updatedAt: Date.now(), status: 'idle', archived: false,
        nextSeq: record.handle.agent.session.seq ?? 0,
      } }
    } catch {
      await record.handle?.dispose().catch(() => {})
      this.sessions.delete(sessionId)
      throw fault('session/create-failed', 'Could not create the session; check the model route and host configuration')
    }
  }

  async resume(params) {
    const id = sessionID(params.sessionId)
    if (this.management.pending.has(id)) throw fault('session/busy', 'Session metadata is being updated')
    if (this.ctx.get('workspaceRegistry')?.archivedSessionIds.includes(id)) throw fault('session/archived', 'Unarchive the session before resuming it')
    const existing = this.sessions.get(id)
    if (existing) {
      if (!existing.handle || existing.busy || existing.modelUpdating || existing.metadataUpdating || existing.handle.agent.status !== 'idle') throw fault('session/busy', 'Session is running or being restored')
      return { session: this.ctx.get('sessionTitle') ? await this.history.describe(existing.handle.agent.session.header, existing) : sessionSummary(existing.handle.agent.session.header, existing) }
    }
    params = this.configuration.sessionRoute(params)
    const provider = text(params.provider, 'provider')
    const model = text(params.model, 'model')
    const effort = params.effort === undefined ? undefined : text(params.effort, 'effort')
    if (this.sessions.size >= this.maxSessions) throw fault('session/limit', 'Host session limit reached')
    const record = { handle: undefined, busy: false, calls: new Map(), route: { provider, model, effort: effort ?? '' }, abort: new AbortController(), selection: { current: undefined, assembled: undefined } }
    this.sessions.set(id, record)
    const task = (async () => {
      try {
        const snapshot = await this.ctx.get('sessionPersistence').stat(id, { signal: record.abort.signal })
        const header = snapshot?.header
        if (!header || header.origin === 'subagent' || snapshot.formatStatus === 'migration-required') throw fault('session/unavailable', 'This stored session cannot be resumed')
        if (typeof header.cwd !== 'string' || !isAbsolute(header.cwd) || !(await stat(header.cwd).catch(() => undefined))?.isDirectory()) throw fault('session/unavailable', 'The stored working directory is unavailable')
        await this.ensureProvider?.(provider)
        const agentOptions = { provider, model, ...(effort === undefined ? {} : { reasoningEffort: this.reasoningEffort(effort) }) }
        await this.ctx.llm.resolveCallConfig(agentOptions)
        record.abort.signal.throwIfAborted()
        record.handle = await this.ctx.agents.resume({ resumeSessionId: id, agentOptions, signal: record.abort.signal, ...(this.installModelSelection ? { setup: (agentCtx) => { this.installModelSelection(agentCtx, record.selection) } } : {}) })
        if (this.stopping) {
          await record.handle.dispose()
          record.handle = undefined
          throw fault('host/stopping', 'Host is stopping')
        }
        if (this.ctx.get('sessionTitle')) {
          await this.ctx.get('sessionPersistence').flush()
          return { session: await this.history.describe(record.handle.agent.session.header, record) }
        }
        return { session: sessionSummary(record.handle.agent.session.header, record) }
      } catch (error) {
        if (record.handle) {
          await record.handle.dispose().catch(() => {})
          record.handle = undefined
        }
        this.sessions.delete(id)
        if (error.kind === 'session/unavailable' || error.kind === 'host/stopping') throw error
        throw fault('session/resume-failed', 'Could not restore the session; check ownership, model route and host configuration')
      }
    })()
    record.resumeTask = task
    return task
  }

  prompt(params) {
    const sessionId = text(params.sessionId, 'sessionId', 128)
    if (!/^[a-zA-Z0-9_-]+$/.test(sessionId)) throw fault('protocol/invalid-params', 'Invalid sessionId', -32602)
    const record = this.sessions.get(sessionId)
    if (!record?.handle) throw fault('session/not-found', 'Session is not owned by this host')
    if (this.ctx.get('workspaceRegistry')?.archivedSessionIds.includes(sessionId)) throw fault('session/archived', 'Unarchive the session before sending a prompt')
    if (!Array.isArray(params.content) || params.content.length === 0 || params.content.length > 64) throw fault('protocol/invalid-params', 'Invalid content', -32602)
    const content = params.content.map((block) => {
      if (block === null || typeof block !== 'object' || Array.isArray(block) || block.type !== 'text') throw fault('protocol/invalid-params', 'Only text content is supported', -32602)
      return { type: 'text', text: text(block.text, 'content text', 1024 * 1024) }
    })
    if (record.busy || record.modelUpdating || record.metadataUpdating || record.handle.agent.status !== 'idle') throw fault('session/busy', 'Session is running')
    record.busy = true
    try {
      const message = this.createUserMessage({ content, source: { kind: 'user' } })
      record.handle.agent.followup(message)
      return { messageId: message.id }
    } catch {
      record.busy = false
      throw fault('session/prompt-failed', 'Could not deliver the prompt')
    }
  }

  shutdown() {
    this.shutdownTask ??= this.dispose()
    return this.shutdownTask
  }

  requestApproval(request, next = () => Promise.resolve('unavailable')) {
    const sessionId = String(request.agent?.session.id ?? '')
    const record = this.sessions.get(sessionId)
    if (!record?.handle || record.handle.agent !== request.agent) return next()
    if (this.stopping) return Promise.resolve('cancelled')
    if (!this.capabilities.includes('approval')) return Promise.resolve('unavailable')
    return this.approvals.ask(request, { sessionId, call: record.calls.get(request.callId) })
  }

  requestQuestion(request, next = () => Promise.reject(Object.assign(new Error('Questions are unavailable'), { name: 'UserQuestionError', code: 'NO_PROVIDER' }))) {
    const sessionId = String(request.agent?.session?.id ?? '')
    if (request.agent !== undefined && this.sessions.get(sessionId)?.handle?.agent !== request.agent) return next()
    if (this.stopping) return Promise.reject(Object.assign(new Error('Questions were cancelled'), { name: 'UserQuestionError', code: 'ASK_CANCELLED' }))
    if (!this.capabilities.includes('question')) return Promise.reject(Object.assign(new Error('Questions are unavailable'), { name: 'UserQuestionError', code: 'NO_PROVIDER' }))
    return this.questions.ask(request, sessionId)
  }

  cancel(params) {
    if (!this.capabilities.includes('session.cancel')) throw fault('protocol/capability-unavailable', 'Session cancellation was not negotiated')
    const sessionId = text(params.sessionId, 'sessionId', 128)
    if (!/^[a-zA-Z0-9_-]+$/.test(sessionId)) throw fault('protocol/invalid-params', 'Invalid sessionId', -32602)
    const record = this.sessions.get(sessionId)
    if (!record?.handle) throw fault('session/not-found', 'Session is not owned by this host')
    if (record.cancelTask) return record.cancelTask
    if (!record.busy && record.handle.agent.status === 'idle') return { cancelled: false, status: 'idle' }
    record.cancelTask = this.cancelAgent(record, sessionId).finally(() => { record.cancelTask = undefined })
    return record.cancelTask
  }

  async cancelAgent(record, sessionId) {
    let timeout
    try {
      record.handle.agent.cancel({ kind: 'user' })
      this.approvals.cancelSession(sessionId)
      this.questions.cancelSession(sessionId)
      await Promise.race([
        record.handle.agent.whenIdle(),
        new Promise((_resolve, reject) => {
          timeout = setTimeout(() => reject(fault('session/cancel-timeout', 'Cancellation is still pending; wait for the host to become idle or retry')), this.cancelTimeoutMs)
        }),
      ])
      record.busy = false
      this.notifyStatus(record, sessionId, 'idle')
      return { cancelled: true, status: 'idle' }
    } catch (error) {
      if (error.kind === 'session/cancel-timeout') throw error
      throw fault('session/cancel-failed', 'Could not stop the turn')
    } finally {
      clearTimeout(timeout)
    }
  }

  notifyStatus(record, sessionId, status) {
    record.statusRevision = (record.statusRevision ?? 0) + 1
    this.notify('session/status', { sessionId, status, ...(this.capabilities.includes('session.resync') ? { statusRevision: record.statusRevision } : {}) })
  }

  async dispose() {
    this.stopping = true
    this.approvals.close()
    this.questions.close()
    const records = [...this.sessions.values()]
    for (const record of records) record.abort?.abort()
    await this.management.drain()
    this.history.close()
    for (const dispose of this.disposers.splice(0)) dispose()
    await Promise.allSettled(records.map((record) => record.resumeTask))
    await this.ctx.get('subagents')?.drainDescendants(records.filter((record) => record.handle).map((record) => record.handle.agent))
    await Promise.all(records.map((record) => record.handle?.dispose()))
    this.sessions.clear()
    return {}
  }
}
