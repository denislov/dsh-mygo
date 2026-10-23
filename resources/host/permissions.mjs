import { sessionID } from './history.mjs'

function fault(kind, message, code = -32000) { return Object.assign(new Error(message), { kind, code }) }
function label(value, limit = 256, empty = false) {
  if (typeof value !== 'string' || !empty && !value.trim() || Buffer.byteLength(value) > limit || /[\u0000-\u001f\u007f]/.test(value)) throw new Error('Invalid permission metadata')
  return value
}
function knobs(value) {
  if (!['read-only', 'workspace-write', 'danger-full-access'].includes(value?.sandbox) || !['ask', 'never'].includes(value.approval)) throw new Error('Invalid permission bundle')
  return { sandbox: value.sandbox, approval: value.approval }
}

export class Permissions {
  constructor(server, maxBytes) { this.server = server; this.maxBytes = Math.min(32 * 1024, maxBytes); this.pending = new Map() }
  supported() {
    const ctx = this.server.ctx
    return ['catalog', 'current', 'resolve', 'set'].every((key) => typeof ctx.get('permissionPresets')?.[key] === 'function') && typeof ctx.get('sandboxPolicy')?.resolve === 'function' && typeof ctx.get('approval')?.effectivePolicy === 'function' && typeof ctx.get('sessionPersistence')?.flush === 'function'
  }
  record(id) {
    const record = this.server.sessions.get(sessionID(id))
    if (!record?.handle) throw fault('session/not-found', 'Session is not owned by this host')
    return record
  }
  snapshot(record) {
    const ctx = this.server.ctx, service = ctx.get('permissionPresets'), session = record.handle.agent.session
    const catalog = service.catalog()
    if (!Array.isArray(catalog.options) || catalog.options.length > 32) throw new Error('Invalid permission catalog')
    const options = catalog.options.map((option) => {
      const id = label(option.value)
      if (id === 'custom') throw new Error('Custom is not selectable')
      const bundle = knobs(service.resolve(id))
      return { id, name: label(option.name), description: label(option.description ?? '', 2048, true), ...bundle, requiresConfirmation: bundle.sandbox === 'danger-full-access' || bundle.approval === 'never' }
    })
    if (new Set(options.map((option) => option.id)).size !== options.length) throw new Error('Duplicate preset')
    const current = label(service.current(session))
    const actual = knobs({ sandbox: ctx.get('sandboxPolicy').resolve({ session }).mode, approval: ctx.get('approval').effectivePolicy(session) })
    if (current !== 'custom' && !options.some((option) => option.id === current && option.sandbox === actual.sandbox && option.approval === actual.approval)) throw new Error('Permission state does not match')
    const result = { sessionId: sessionID(String(session.id)), current, ...actual, nextSeq: session.seq, options }
    if (!Number.isSafeInteger(result.nextSeq) || result.nextSeq < 0 || Buffer.byteLength(JSON.stringify(result)) > this.maxBytes) throw new Error('Permission snapshot exceeds budget')
    return result
  }
  idle(record) {
    if (record.busy || record.modelUpdating || record.metadataUpdating || record.handle.agent.status !== 'idle') throw fault('session/busy', 'Wait for the session to become idle')
  }
  async get(params) {
    const record = this.record(params.sessionId)
    await this.pending.get(params.sessionId)?.catch(() => {})
    this.idle(record)
    try {
      await this.server.ctx.get('sessionPersistence').flush()
      if (this.server.stopping) throw new Error('Host is stopping')
      const result = this.snapshot(record)
      record.permissionUncertain = false
      return result
    } catch { throw fault('permission/unavailable', 'Could not read session permissions; refresh to retry') }
  }
  select(params) {
    const id = sessionID(params.sessionId), record = this.record(id)
    if (typeof params.preset !== 'string' || !params.preset || Buffer.byteLength(params.preset) > 256 || params.preset === 'custom' || !Number.isSafeInteger(params.expectedNextSeq) || params.expectedNextSeq < 0 || params.confirmDangerous !== undefined && typeof params.confirmDangerous !== 'boolean') throw fault('protocol/invalid-params', 'Invalid permission selection', -32602)
    this.idle(record)
    if (record.permissionUncertain) throw fault('permission/refresh-required', 'Refresh permissions before retrying')
    let before
    try { before = this.snapshot(record) } catch { throw fault('permission/unavailable', 'Could not read session permissions') }
    const option = before.options.find((option) => option.id === params.preset)
    if (!option) throw fault('protocol/invalid-params', 'Permission preset is unavailable', -32602)
    if (before.nextSeq !== params.expectedNextSeq) throw fault('permission/conflict', 'Session changed; refresh permissions before retrying')
    if (option.requiresConfirmation && params.confirmDangerous !== true) throw fault('permission/confirmation-required', 'Explicit confirmation is required for this permission bundle')
    record.metadataUpdating = true
    const task = Promise.resolve().then(async () => {
      if (this.server.stopping) throw new Error('Host is stopping')
      this.server.ctx.get('permissionPresets').set(record.handle.agent.session, params.preset)
      await this.server.ctx.get('sessionPersistence').flush()
      const result = this.snapshot(record)
      if (result.current !== option.id || result.sandbox !== option.sandbox || result.approval !== option.approval) throw new Error('Permission selection was not applied')
      return result
    }).catch(() => {
      record.permissionUncertain = true
      throw fault('permission/write-failed', 'Could not confirm permission selection; refresh before continuing')
    }).finally(() => { record.metadataUpdating = false; this.pending.delete(id) })
    this.pending.set(id, task)
    return task
  }
  async drain() { await Promise.allSettled([...this.pending.values()]) }
}
