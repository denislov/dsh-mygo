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
import { AssistantStreamBaseline } from './stream-baseline.mjs'
import { Permissions } from './permissions.mjs'
import { admitPrompt, assertImageRoute, imagePolicy, promptContent } from './attachments.mjs'
import { SubagentInspection } from './subagents.mjs'

const identity = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8'))
if (!Number.isSafeInteger(identity.dshMygo?.protocolVersion)) {
  throw new Error('The host package must declare dshMygo.protocolVersion')
}
export const protocolVersion = identity.dshMygo.protocolVersion
export const releaseVersion = identity.version
export const appCompatibility = identity.dshMygo.appCompatibility
if (parseAppVersion(appCompatibility?.min) === undefined || parseAppVersion(appCompatibility?.maxExclusive) === undefined) {
  throw new Error('The host package must declare dshMygo.appCompatibility.min/maxExclusive')
}

function parseAppVersion(value) {
  if (typeof value !== 'string' || !/^\d+\.\d+\.\d+$/.test(value)) return undefined
  return value.split('.').map((part) => Number(part))
}

function compareAppVersion(left, right) {
  const a = parseAppVersion(left)
  const b = parseAppVersion(right)
  if (a === undefined || b === undefined) return undefined
  for (let i = 0; i < 3; i += 1) {
    if (a[i] !== b[i]) return a[i] < b[i] ? -1 : 1
  }
  return 0
}

function supportsAppVersion(value) {
  const lower = compareAppVersion(value, appCompatibility.min)
  const upper = compareAppVersion(value, appCompatibility.maxExclusive)
  return lower !== undefined && upper !== undefined && lower >= 0 && upper < 0
}

const SUBAGENT_STOP_REASONS = new Set(['completed', 'aborted', 'error', 'max-tokens', 'refusal'])
const maxTrackedSubagentRuns = 256

function subagentStopReason(reason) {
  return SUBAGENT_STOP_REASONS.has(reason) ? reason : 'error'
}

function subagentIdentifier(value) {
  return typeof value === 'string' && /^[A-Za-z0-9._:-]{1,128}$/.test(value) ? value : undefined
}
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

const CONFIGURATION_BLOCKING_METHODS = new Set([
  'session/create', 'session/resume', 'session/prompt', 'session/rename', 'session/archive', 'model/select', 'permission/select',
])
const PERMISSION_SENSITIVE_METHODS = new Set([
  'session/resume', 'session/prompt', 'session/rename', 'session/archive', 'model/select',
])

function requireCapability(server, capability, message, invoke) {
  if (!server.capabilities.includes(capability)) throw fault('protocol/capability-unavailable', message)
  return invoke()
}

function historyRoute(server, method, params) {
  return requireCapability(server, 'session.history', 'Session history was not negotiated', () => {
    if (method === 'session/history') {
      if (params.direction === 'backward' && !server.capabilities.includes('session.history-window')) throw fault('protocol/invalid-params', 'Backward history was not negotiated', -32602)
      if (params.live !== undefined && typeof params.live !== 'boolean') throw fault('protocol/invalid-params', 'Invalid live history flag', -32602)
      if (params.live && !server.capabilities.includes('session.resync')) throw fault('protocol/capability-unavailable', 'Live history was not negotiated')
      if (params.assistantStream !== undefined && typeof params.assistantStream !== 'boolean') throw fault('protocol/invalid-params', 'Invalid stream baseline flag', -32602)
      if (params.assistantStream && (!params.live || !server.capabilities.includes('session.assistant-stream-baseline'))) throw fault('protocol/capability-unavailable', 'Stream baseline was not negotiated for live history')
    }
    if (method === 'session/list') return server.history.list(params)
    return method === 'session/resume' ? server.resume(params) : server.history.history(params)
  })
}

// One explicit table keeps method -> capability -> handler order visible. The
// lifecycle handlers stay on MygoServer; this table only routes and gates.
const routes = {
  'subagent/list': (server, params) => requireCapability(server, 'subagent.inspection', 'Subagent inspection was not negotiated', () => server.subagentInspection.list(params)),
  'subagent/history': (server, params) => requireCapability(server, 'subagent.inspection', 'Subagent inspection was not negotiated', () => server.subagentInspection.history(params)),
  'session/release': (server, params) => requireCapability(server, 'session.release', 'Session release was not negotiated', () => server.release(params)),
  'permission/get': (server, params) => requireCapability(server, 'session.permissions', 'Session permissions were not negotiated', () => server.permissions.get(params)),
  'permission/select': (server, params) => requireCapability(server, 'session.permissions', 'Session permissions were not negotiated', () => server.permissions.select(params)),
  'session/rename': (server, params) => requireCapability(server, 'session.rename', 'Session management was not negotiated', () => server.management.rename(params)),
  'session/archive': (server, params) => requireCapability(server, 'session.archive', 'Session management was not negotiated', () => server.management.archive(params)),
  'configuration/get': (server) => requireCapability(server, 'settings.configuration', 'Configuration was not negotiated', () => server.configuration.get()),
  'configuration/default-model': (server, params) => requireCapability(server, 'settings.configuration', 'Configuration was not negotiated', () => server.configuration.saveDefaults(params)),
  'configuration/credential': (server, params) => requireCapability(server, 'settings.configuration', 'Configuration was not negotiated', () => server.configuration.saveCredential(params)),
  'session/create': (server, params) => server.create(params),
  'model/list': (server, params) => requireCapability(server, 'model.selection', 'Model selection was not negotiated', () => server.models.list(params)),
  'model/select': (server, params) => requireCapability(server, 'model.selection', 'Model selection was not negotiated', () => server.models.select(params)),
  'session/list': (server, params, method) => historyRoute(server, method, params),
  'session/resume': (server, params, method) => historyRoute(server, method, params),
  'session/history': (server, params, method) => historyRoute(server, method, params),
  'session/prompt': (server, params) => server.prompt(params),
  'session/cancel': (server, params) => server.cancel(params),
  'approval/respond': (server, params) => requireCapability(server, 'approval', 'Approval was not negotiated', () => server.approvals.respond(params)),
  'question/respond': (server, params) => requireCapability(server, 'question', 'Questions were not negotiated', () => server.questions.respond(params)),
  'question/cancel': (server, params) => requireCapability(server, 'question', 'Questions were not negotiated', () => server.questions.cancel(params)),
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
    // runId -> parent/child identity learned at start, because a settled child
    // may already be gone from the agent registry when subagent/end arrives.
    this.subagentRuns = new Map()
    this.history = new SessionHistory(ctx, this.sessions, Math.floor(maxFrameBytes / 2), () => this.capabilities.includes('session.assistant-stream-baseline-reasoning'))
    this.subagentInspection = new SubagentInspection(this, maxFrameBytes)
    this.models = new ModelSettings(this, Math.floor(maxFrameBytes / 2))
    this.configuration = new Configuration(this, credentialRef)
    this.management = new SessionManagement(this)
    this.permissions = new Permissions(this, Math.floor(maxFrameBytes / 2))
    this.attachmentsPolicy = undefined
    this.initialized = false
    this.stopping = false
    this.disposers = [
      ctx.on('agent/assistant-stream', ({ agent, frame }) => {
        if (this.stopping || !this.capabilities.includes('session.assistant-stream')) return
        const sessionId = String(agent.session.id)
        const record = this.sessions.get(sessionId)
        if (record?.handle?.agent !== agent) return
        if (this.capabilities.includes('session.assistant-stream-baseline')) {
          record.assistantStream ??= new AssistantStreamBaseline()
          record.assistantStream.accept(frame)
        }
        this.notify('session/assistant-stream', { sessionId, frame })
      }),
      ctx.on('session/event', (session, event) => {
        const record = this.sessions.get(String(session.id))
        if (record?.handle?.agent.session !== session) return
        record.assistantStream?.commit(event)
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
      ctx.on('subagent/start', (info) => {
        const payload = this.subagentPayload(info)
        if (!payload) return
        this.subagentRuns.set(String(info.runId), payload)
        if (this.subagentRuns.size > maxTrackedSubagentRuns) this.subagentRuns.delete(this.subagentRuns.keys().next().value)
        this.notify('subagent/started', payload)
      }),
      ctx.on('subagent/end', (info) => {
        const runId = String(info?.runId)
        const cached = this.subagentRuns.get(runId)
        const payload = cached ?? this.subagentPayload(info)
        if (!payload) return
        this.subagentRuns.delete(runId)
        this.notify('subagent/finished', { ...payload, reason: subagentStopReason(info?.stopReason) })
      }),
    ]
  }

  async handle(method, params) {
    if (method === 'shutdown') return this.shutdown()
    if (this.stopping) throw fault('host/stopping', 'Host is stopping')
    if (method === 'initialize') return this.initialize(params)
    if (!this.initialized) throw fault('protocol/not-initialized', 'Initialize the host first')
    if (this.configuration.pending && CONFIGURATION_BLOCKING_METHODS.has(method)) throw fault('settings/busy', 'A configuration write is still pending')
    if (PERMISSION_SENSITIVE_METHODS.has(method) && this.sessions.get(params.sessionId)?.permissionUncertain) throw fault('permission/refresh-required', 'Refresh permissions before continuing')
    const route = routes[method]
    if (route === undefined) throw fault('protocol/method-not-found', 'Unknown channel method', -32601)
    return route(this, params, method)
  }

  initialize(params) {
    if (this.initialized) throw fault('protocol/already-initialized', 'Host is already initialized')
    if (!Number.isSafeInteger(params.protocolVersion)) throw fault('protocol/invalid-params', 'Invalid protocolVersion', -32602)
    if (params.protocolVersion !== protocolVersion) throw fault('protocol/version-mismatch', 'Incompatible channel protocol version')
    if (this.dshVersion !== supportedDshVersion) throw fault('protocol/runtime-mismatch', 'Incompatible DSH runtime version')
    if (params.client === null || typeof params.client !== 'object' || Array.isArray(params.client)) throw fault('protocol/invalid-params', 'Invalid client', -32602)
    for (const field of ['name', 'version', 'platform', 'locale']) text(params.client[field], `client.${field}`)
    if (!supportsAppVersion(params.client.version)) throw fault('protocol/release-mismatch', 'UI and host release versions are incompatible')
    if (!Array.isArray(params.capabilities) || params.capabilities.length > 64) throw fault('protocol/invalid-params', 'Invalid capabilities', -32602)
    for (const capability of params.capabilities) text(capability, 'capability')
    this.initialized = true
    this.capabilities = ['session.cancel', 'approval', 'question', 'session.assistant-stream'].filter((capability) => params.capabilities.includes(capability))
    const persistence = this.ctx.get('sessionPersistence')
    if (params.capabilities.includes('subagent.inspection') && this.subagentInspection.supported()) this.capabilities.push('subagent.inspection')
    if (params.capabilities.includes('session.permissions') && this.permissions.supported()) this.capabilities.push('session.permissions')
    if (params.capabilities.includes('settings.configuration') && this.configuration.supported()) this.capabilities.push('settings.configuration')
    if (params.capabilities.includes('model.selection') && typeof this.installModelSelection === 'function' && ['listProviders', 'listModels', 'resolveModelInfo', 'resolveCallConfig'].every((method) => typeof this.ctx.llm[method] === 'function')) this.capabilities.push('model.selection')
    if (params.capabilities.includes('session.history') && ['list', 'stat', 'open', 'flush'].every((method) => typeof persistence?.[method] === 'function') && typeof this.ctx.agents.resume === 'function') {
      this.capabilities.push('session.history')
      for (const capability of ['session.history-window', 'session.release']) if (params.capabilities.includes(capability)) this.capabilities.push(capability)
      if (params.capabilities.includes('session.resync')) this.capabilities.push('session.resync')
    }
    if (params.capabilities.includes('session.assistant-stream-baseline') && this.capabilities.includes('session.resync') && this.capabilities.includes('session.assistant-stream')) this.capabilities.push('session.assistant-stream-baseline')
    if (params.capabilities.includes('session.assistant-stream-baseline-reasoning') && this.capabilities.includes('session.assistant-stream-baseline')) this.capabilities.push('session.assistant-stream-baseline-reasoning')
    if (params.capabilities.includes('attachments')) {
      const policy = imagePolicy(this.ctx.get('attachments'))
      if (policy !== undefined && typeof this.ctx.get('llm')?.resolveModelInfo === 'function') {
        this.capabilities.push('attachments')
        this.attachmentsPolicy = policy
      }
    }
    if (['stat', 'open', 'flush'].every((method) => typeof persistence?.[method] === 'function')) {
      if (params.capabilities.includes('session.rename') && typeof this.ctx.get('sessionTitle')?.rename === 'function') this.capabilities.push('session.rename')
      const registry = this.ctx.get('workspaceRegistry')
      if (params.capabilities.includes('session.archive') && typeof registry?.archiveSession === 'function' && typeof registry?.unarchiveSession === 'function') this.capabilities.push('session.archive')
    }
    const result = {
      server: {
        name: 'dsh-mygo-host', version: releaseVersion, dshVersion: this.dshVersion,
        appMin: appCompatibility.min, appMaxExclusive: appCompatibility.maxExclusive,
      },
      protocolVersion, capabilities: this.capabilities,
    }
    if (this.attachmentsPolicy !== undefined) result.attachments = { image: this.attachmentsPolicy }
    return result
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
    const record = { handle: undefined, busy: false, metadataUpdating: true, calls: new Map(), route: { provider, model, effort: effort ?? '' }, selection: { current: undefined, assembled: undefined } }
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
    } finally { record.metadataUpdating = false }
  }

  async resume(params) {
    const id = sessionID(params.sessionId)
    if (this.management.pending.has(id)) throw fault('session/busy', 'Session metadata is being updated')
    if (this.ctx.get('workspaceRegistry')?.archivedSessionIds.includes(id)) throw fault('session/archived', 'Unarchive the session before resuming it')
    const existing = this.sessions.get(id)
    if (existing) {
      if (!existing.handle || existing.busy || existing.modelUpdating || existing.metadataUpdating || existing.handle.agent.status !== 'idle') throw fault('session/busy', 'Session is running or being restored')
      existing.catalogReaders = (existing.catalogReaders ?? 0) + 1
      try { return { session: this.ctx.get('sessionTitle') ? await this.history.describe(existing.handle.agent.session.header, existing) : sessionSummary(existing.handle.agent.session.header, existing) } }
      finally { existing.catalogReaders-- }
    }
    params = this.configuration.sessionRoute(params)
    const provider = text(params.provider, 'provider')
    const model = text(params.model, 'model')
    const effort = params.effort === undefined ? undefined : text(params.effort, 'effort')
    if (this.sessions.size >= this.maxSessions) throw fault('session/limit', 'Host session limit reached')
    const record = { handle: undefined, busy: false, metadataUpdating: true, calls: new Map(), route: { provider, model, effort: effort ?? '' }, abort: new AbortController(), selection: { current: undefined, assembled: undefined } }
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
        await this.ctx.get('sessionProjectionCache')?.write?.(record.handle.agent.session).catch(() => {})
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
      } finally { record.metadataUpdating = false }
    })()
    record.resumeTask = task
    return task
  }

  async prompt(params) {
    const sessionId = text(params.sessionId, 'sessionId', 128)
    if (!/^[a-zA-Z0-9_-]+$/.test(sessionId)) throw fault('protocol/invalid-params', 'Invalid sessionId', -32602)
    const record = this.sessions.get(sessionId)
    if (!record?.handle) throw fault('session/not-found', 'Session is not owned by this host')
    if (this.ctx.get('workspaceRegistry')?.archivedSessionIds.includes(sessionId)) throw fault('session/archived', 'Unarchive the session before sending a prompt')
    const { parts, hasImage } = promptContent(params.content, this.attachmentsPolicy)
    if (record.busy || record.modelUpdating || record.metadataUpdating || record.handle.agent.status !== 'idle') throw fault('session/busy', 'Session is running')
    record.busy = true
    try {
      let content = parts
      if (hasImage) {
        await assertImageRoute(this.ctx, record.route)
        content = await admitPrompt(this.ctx, parts)
      }
      const message = this.createUserMessage({ content, source: { kind: 'user' } })
      record.handle.agent.followup(message)
      return { messageId: message.id }
    } catch (error) {
      record.busy = false
      if (typeof error?.kind === 'string') throw error
      throw fault('session/prompt-failed', 'Could not deliver the prompt')
    }
  }

  release(params) {
    const id = sessionID(params.sessionId)
    const record = this.sessions.get(id)
    if (!record) return {}
    if (record.releaseTask) return record.releaseTask
    const interacting = [...this.approvals.pending.values(), ...this.questions.pending.values()].some(entry => entry.sessionId === id)
    if (!record.handle || record.busy || record.modelUpdating || record.metadataUpdating && !record.releaseFailed || record.cancelTask || record.historyReaders || record.catalogReaders || record.permissionUncertain || record.handle.agent.status !== 'idle' || interacting || this.management.pending.has(id) || this.configuration.pending) throw fault('session/busy', 'Wait for all session activity to finish before releasing it')
    record.metadataUpdating = true
    const task = (async () => {
      try {
        await this.ctx.get('sessionPersistence').flush()
        await this.ctx.get('sessionProjectionCache')?.write?.(record.handle.agent.session).catch(() => {})
        // Parent idle alone does not prove its background jobs/children are
        // idle. Use the same public activity seam as archive admission.
        const activity = await this.ctx.waterfall?.('workspace/session-activity', { sessionId: id }, () => Promise.resolve([])) ?? []
        if (activity.length || record.busy || record.handle.agent.status !== 'idle') throw fault('session/busy', 'Wait for background session activity before releasing it')
        await this.ctx.get('subagents')?.drainDescendants([record.handle.agent])
        await record.handle.dispose()
        record.handle = undefined
        this.sessions.delete(id)
        for (const [key, cursor] of this.history.cursors) if (cursor.id === id) this.history.cursors.delete(key)
        return {}
      } catch (error) {
        if (error.kind === 'session/busy') { record.metadataUpdating = false; throw error }
        record.releaseFailed = true
        throw fault('session/release-failed', 'Could not release the session; retry release or restart the host')
      } finally { record.releaseTask = undefined }
    })()
    record.releaseTask = task
    return task
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

  // Only locally resolvable children are forwarded: the parent session must be
  // one this Host owns, and the descriptor link comes from the child header.
  subagentPayload(info) {
    if (this.stopping || info === null || typeof info !== 'object') return undefined
    const childId = subagentIdentifier(info.id)
    if (childId === undefined) return undefined
    let parentSessionId
    try {
      parentSessionId = subagentIdentifier(String(this.ctx.get('agents')?.get?.(info.id)?.session?.header?.parentSession))
    } catch {
      return undefined
    }
    if (parentSessionId === undefined || !this.sessions.get(parentSessionId)?.handle?.agent) return undefined
    const provider = subagentIdentifier(info.provider)
    return { parentSessionId, childSessionId: childId, ...(provider === undefined ? {} : { provider }) }
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
    await this.subagentInspection.close()
    await this.management.drain()
    await this.permissions.drain()
    await Promise.allSettled(records.map(record => record.releaseTask))
    await Promise.allSettled(records.filter(record => record.handle).map(record => this.ctx.get('sessionProjectionCache')?.write?.(record.handle.agent.session)))
    this.history.close()
    for (const dispose of this.disposers.splice(0)) dispose()
    await Promise.allSettled(records.map((record) => record.resumeTask))
    await this.ctx.get('subagents')?.drainDescendants(records.filter((record) => record.handle).map((record) => record.handle.agent))
    await Promise.all(records.map((record) => record.handle?.dispose()))
    this.sessions.clear()
    return {}
  }
}
