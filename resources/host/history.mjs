import { randomUUID } from 'node:crypto'

function fault(kind, message, code = -32000) {
  return Object.assign(new Error(message), { kind, code })
}

export function sessionID(value) {
  if (typeof value !== 'string' || value.length < 1 || value.length > 128 || /[^a-zA-Z0-9_-]/.test(value)) {
    throw fault('protocol/invalid-params', 'Invalid sessionId', -32602)
  }
  return value
}

export function sessionSummary(header, record) {
  return {
    id: sessionID(header.id), title: '', cwd: header.cwd ?? '',
    provider: record?.route?.provider ?? '', model: record?.route?.model ?? '', effort: record?.route?.effort ?? '',
    createdAt: header.createdAt, updatedAt: header.createdAt,
    status: record?.handle ? (record.busy ? 'running' : record.handle.agent.status) : 'stored',
    archived: false,
    ...(header.parentSession === undefined ? {} : { parentId: header.parentSession }),
    ...(record?.handle ? { nextSeq: record.handle.agent.session.seq } : {}),
  }
}

export class SessionHistory {
  constructor(ctx, sessions, maxPageBytes) {
    this.ctx = ctx
    this.sessions = sessions
    this.maxPageBytes = maxPageBytes
    this.cursors = new Map()
  }

  limit(params) {
    const limit = params.limit === undefined ? 64 : params.limit
    if (!Number.isSafeInteger(limit) || limit < 1 || limit > 256) throw fault('protocol/invalid-params', 'Invalid limit', -32602)
    return limit
  }

  cursor(value, kind, id) {
    const saved = typeof value === 'string' && value.length <= 128 ? this.cursors.get(value) : undefined
    if (!saved || saved.kind !== kind || saved.id !== id || saved.expires < Date.now()) {
      throw fault('session/invalid-cursor', 'The cursor is invalid or expired; restart pagination')
    }
    return saved
  }

  save(value) {
    for (const [key, cursor] of this.cursors) {
      if (cursor.expires < Date.now()) this.cursors.delete(key)
    }
    if (this.cursors.size >= 32) this.cursors.delete(this.cursors.keys().next().value)
    const key = randomUUID()
    this.cursors.set(key, { ...value, expires: Date.now() + 5 * 60 * 1000 })
    return key
  }

  page(values, limit) {
    const result = []
    let bytes = 2
    for (const value of values.slice(0, limit)) {
      const size = Buffer.byteLength(JSON.stringify(value)) + 1
      if (bytes + size > this.maxPageBytes) {
        if (result.length === 0) throw fault('session/page-too-large', 'A history event or session summary exceeds the channel page budget')
        break
      }
      bytes += size
      result.push(value)
    }
    return result
  }

  async list(params) {
    const limit = this.limit(params)
    let state
    if (params.cursor !== undefined) {
      state = this.cursor(params.cursor, 'list')
    } else {
      let snapshots
      try { snapshots = await this.ctx.get('sessionPersistence').list() } catch {
        throw fault('session/list-failed', 'Could not list stored sessions')
      }
      const sessions = snapshots.filter(({ header, formatStatus }) => header.origin !== 'subagent' && formatStatus !== 'migration-required')
        .map(({ header }) => sessionSummary(header, this.sessions.get(header.id)))
        .sort((left, right) => right.createdAt - left.createdAt || left.id.localeCompare(right.id))
      if (sessions.length > 1000 || Buffer.byteLength(JSON.stringify(sessions)) > 256 * 1024) {
        throw fault('session/catalog-too-large', 'The stored session catalog exceeds the supported snapshot budget')
      }
      state = { kind: 'list', sessions, offset: 0 }
    }
    const sessions = this.page(state.sessions.slice(state.offset, state.offset + limit), limit)
    const offset = state.offset + sessions.length
    return { sessions, ...(offset < state.sessions.length ? { nextCursor: this.save({ ...state, offset }) } : {}) }
  }

  async history(params) {
    const id = sessionID(params.sessionId)
    const limit = this.limit(params)
    if (params.direction !== undefined && params.direction !== 'forward') throw fault('protocol/invalid-params', 'Only forward history is supported', -32602)
    const record = this.sessions.get(id)
    if (!record?.handle) throw fault('session/not-found', 'Session is not owned by this host')
    if (record.busy || record.modelUpdating || record.handle.agent.status !== 'idle') throw fault('session/busy', 'Wait for the session to become idle before reading history')
    const state = params.cursor === undefined ? { kind: 'history', id, offset: 0, nextSeq: record.handle.agent.session.seq } : this.cursor(params.cursor, 'history', id)
    const persistence = this.ctx.get('sessionPersistence')
    let events
    try {
      await persistence.flush()
      const handle = await persistence.open(id, 'read')
      try {
        events = (await handle.read(state.offset, Math.min(limit, state.nextSeq - state.offset))).events
      } finally { await handle.close() }
    } catch {
      throw fault('session/history-failed', 'Could not read stored history')
    }
    const expected = Math.min(limit, state.nextSeq - state.offset)
    if (events.length !== expected || events.some((event, index) => event.seq !== state.offset + index)) {
      throw fault('session/history-failed', 'Stored history does not match the captured sequence boundary')
    }
    const page = this.page(events, limit)
    const offset = state.offset + page.length
    const hasMore = offset < state.nextSeq
    return { events: page, nextSeq: state.nextSeq, hasMore, ...(hasMore ? { nextCursor: this.save({ ...state, offset }) } : {}) }
  }

  close() { this.cursors.clear() }
}
