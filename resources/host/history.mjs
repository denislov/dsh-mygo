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

export function sessionSummary(header, record, metadata = record?.metadata ?? {}) {
  return {
    id: sessionID(header.id), title: metadata.title || header.id.slice(0, 8), cwd: header.cwd ?? '',
    provider: record?.route?.provider ?? '', model: record?.route?.model ?? '', effort: record?.route?.effort ?? '',
    createdAt: header.createdAt, updatedAt: metadata.updatedAt ?? header.createdAt,
    status: record?.handle ? (record.busy ? 'running' : record.handle.agent.status) : 'stored',
    archived: metadata.archived ?? false,
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
    this.metadata = new Map()
  }

  async describe(header, record, snapshot) {
    const persistence = this.ctx.get('sessionPersistence')
    if (!persistence) return sessionSummary(header, record)
    snapshot ??= await persistence.stat(header.id)
    const cached = this.metadata.get(header.id)
    const seq = record?.handle?.agent.session.seq
    let metadata
    if (snapshot?.revision !== undefined && cached?.revision === snapshot.revision && cached.seq === seq) {
      metadata = cached.value
    } else {
      const handle = await persistence.open(header.id, 'read')
      metadata = { title: '', updatedAt: header.createdAt }
      let offset = 0
      let bytes = 0
      const boundary = snapshot?.eventCount
      try {
        for (;;) {
          const length = boundary === undefined ? 64 : Math.min(64, boundary - offset)
          if (length === 0) break
          const { events } = await handle.read(offset, length)
          if (events.length > length || boundary !== undefined && events.length !== length) throw new Error('Invalid summary prefix')
          for (const event of events) {
            bytes += Buffer.byteLength(JSON.stringify(event))
            if (offset >= 20000 || bytes > 32 * 1024 * 1024) throw fault('session/catalog-too-large', 'Session metadata exceeds the supported history budget')
            if (event.seq !== offset || !Number.isSafeInteger(event.time) || event.time < 0) throw new Error('Invalid summary event')
            metadata.updatedAt = Math.max(metadata.updatedAt, event.time)
            if (event.type === 'session/title') {
              const title = event.data?.title
              if (typeof title !== 'string' || Buffer.byteLength(title) > 1024 || /[\u0000-\u001f\u007f]/.test(title)) throw new Error('Invalid stored title')
              metadata.title = title
            }
            offset++
          }
          if (events.length < length) break
        }
      } finally { await handle.close() }
      if (this.metadata.size >= 1000) this.metadata.delete(this.metadata.keys().next().value)
      this.metadata.set(header.id, { revision: snapshot?.revision, seq, value: metadata })
    }
    const archived = this.ctx.get('workspaceRegistry')?.archivedSessionIds.includes(header.id) ?? false
    if (record) record.metadata = { ...metadata, archived }
    return sessionSummary(header, record, { ...metadata, archived })
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

  page(values, limit, maxBytes = this.maxPageBytes) {
    const result = []
    let bytes = 2
    for (const value of values.slice(0, limit)) {
      const size = Buffer.byteLength(JSON.stringify(value)) + 1
      if (bytes + size > maxBytes) {
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
    if (params.includeArchived !== undefined && typeof params.includeArchived !== 'boolean') throw fault('protocol/invalid-params', 'Invalid includeArchived', -32602)
    let state
    if (params.cursor !== undefined) {
      state = this.cursor(params.cursor, 'list')
    } else {
      let snapshots
      try {
        await this.ctx.get('sessionPersistence').flush()
        snapshots = await this.ctx.get('sessionPersistence').list()
      } catch {
        throw fault('session/list-failed', 'Could not list stored sessions')
      }
      const visible = snapshots.filter(({ header, formatStatus }) => header.origin !== 'subagent' && formatStatus !== 'migration-required')
      if (visible.length > 1000) throw fault('session/catalog-too-large', 'The stored session catalog exceeds the supported snapshot budget')
      const sessions = []
      try {
        for (const snapshot of visible) {
          const id = snapshot.header.id
          if (!params.includeArchived && this.ctx.get('workspaceRegistry')?.archivedSessionIds.includes(id)) continue
          sessions.push(await this.describe(snapshot.header, this.sessions.get(id), snapshot))
        }
      } catch (error) {
        if (error.kind === 'session/catalog-too-large') throw error
        throw fault('session/list-failed', 'Could not read stored session metadata')
      }
      sessions.sort((left, right) => right.updatedAt - left.updatedAt || left.id.localeCompare(right.id))
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
    const live = params.live === true
    if (record.modelUpdating || record.metadataUpdating || !live && (record.busy || record.handle.agent.status !== 'idle')) throw fault('session/busy', 'Wait for the session to become idle before reading history')
    const state = params.cursor === undefined ? {
      kind: 'history', id, offset: 0, nextSeq: record.handle.agent.session.seq, live,
      ...(live ? { status: record.busy || record.handle.agent.status !== 'idle' ? 'running' : 'idle', statusRevision: record.statusRevision ?? 0 } : {}),
      ...(params.assistantStream ? { assistantStream: record.assistantStream?.snapshot() ?? { revision: 0 } } : {}),
    } : this.cursor(params.cursor, 'history', id)
    if (state.live !== live) throw fault('session/invalid-cursor', 'History cursor mode does not match')
    if ((state.assistantStream !== undefined) !== (params.assistantStream === true)) throw fault('session/invalid-cursor', 'History cursor stream mode does not match')
    const baselineBytes = state.assistantStream ? Buffer.byteLength(JSON.stringify(state.assistantStream)) : 0
    if (baselineBytes > Math.min(128 * 1024, this.maxPageBytes)) throw fault('session/page-too-large', 'Stream baseline exceeds the channel snapshot budget')
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
    const page = this.page(events, limit, this.maxPageBytes - baselineBytes)
    const offset = state.offset + page.length
    const hasMore = offset < state.nextSeq
    return { events: page, nextSeq: state.nextSeq, hasMore, ...(live ? { status: state.status, statusRevision: state.statusRevision } : {}), ...(state.assistantStream ? { assistantStream: state.assistantStream } : {}), ...(hasMore ? { nextCursor: this.save({ ...state, offset }) } : {}) }
  }

  close() { this.cursors.clear(); this.metadata.clear() }
}
