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

// A Session stops being a blank New conversation once its log holds a prompt
// (a user message or a turn start). The header alone and other system events
// do not count. DSH's list projection folds turn/start; the user-message check
// keeps the fallback correct before that event commits.
function activeSessionBlank(session) {
  if ((session.seq ?? 0) === 0) return true
  if (typeof session.snapshotEvents !== 'function') return false
  try {
    return !session.snapshotEvents().some(event => event.type === 'user/message' || event.type === 'turn/start')
  } catch {
    return false
  }
}

export function sessionSummary(header, record, metadata = record?.metadata ?? {}) {
  // describe() may have read DSH's list projection; only sessions summarized
  // without metadata fall back to the live log snapshot.
  const blank = metadata.title
    ? false
    : typeof metadata.blank === 'boolean'
      ? metadata.blank
      : record?.handle
        ? activeSessionBlank(record.handle.agent.session)
        : false
  return {
    id: sessionID(header.id), title: metadata.title || '', cwd: header.cwd ?? '',
    provider: record?.route?.provider ?? '', model: record?.route?.model ?? '', effort: record?.route?.effort ?? '',
    createdAt: header.createdAt, updatedAt: metadata.updatedAt ?? header.createdAt,
    status: record?.handle ? (record.busy ? 'running' : record.handle.agent.status) : 'stored',
    archived: metadata.archived ?? false, blank,
    ...(metadata.incomplete ? { metadataIncomplete: true } : {}),
    ...(metadata.cached ? { metadataCached: true } : {}),
    ...(header.parentSession === undefined ? {} : { parentId: header.parentSession }),
    ...(record?.handle ? { nextSeq: record.handle.agent.session.seq } : {}),
  }
}

export class SessionHistory {
  constructor(ctx, sessions, maxPageBytes, reasoningBaseline = () => false) {
    this.ctx = ctx
    this.sessions = sessions
    this.maxPageBytes = maxPageBytes
    this.reasoningBaseline = reasoningBaseline
    this.cursors = new Map()
    this.metadata = new Map()
    this.blank = new Map()
  }

  async describe(header, record, snapshot) {
    const persistence = this.ctx.get('sessionPersistence')
    if (!persistence) return sessionSummary(header, record)
    const projections = this.ctx.get('sessionProjections')
    const cache = this.ctx.get('sessionProjectionCache')
    if (projections && cache) {
      const live = record?.handle?.agent.session
      let block
      try { block = live ? projections.snapshot(live, ['mygoCatalog', 'sessionListMetadata']) : cache.cachedSnapshot(header, ['mygoCatalog', 'sessionListMetadata']) } catch { /* A corrupt derived row must not hide the catalog. */ }
      const value = block?.values?.mygoCatalog
      const valid = value && typeof value.title === 'string' && Buffer.byteLength(value.title) <= 1024 && !/[\u0000-\u001f\u007f]/.test(value.title) && Number.isSafeInteger(value.updatedAt) && value.updatedAt >= header.createdAt
      // DSH's own list projection already folds turn/start into blank. It is
      // authoritative whenever it exists; the bounded probe below only serves
      // caches written before that projection was stored.
      const listMetadata = block?.values?.sessionListMetadata
      const projectedBlank = typeof listMetadata?.blank === 'boolean' ? listMetadata.blank : undefined
      // Older caches written by DSH clients may carry the public title
      // projection but not this app's mygoCatalog. Use it as a read-only
      // fallback so a named conversation keeps its name after migration.
      let legacyTitle = typeof block?.values?.title === 'string' ? block.values.title : ''
      if (!valid && legacyTitle === '') {
        try {
          const legacy = live ? projections.snapshot(live, ['title']) : cache.cachedSnapshot(header, ['title'])
          if (typeof legacy?.values?.title === 'string') legacyTitle = legacy.values.title
        } catch { /* The title projection is optional. */ }
      }
      const title = valid && value.title !== '' ? value.title : legacyTitle
      // Without the list projection, an untitled cold Session needs a bounded
      // prefix probe: caches may predate a reset log or miss mygoCatalog
      // entirely, so neither updatedAt nor an absent row proves blankness.
      const blank = title !== ''
        ? false
        : projectedBlank ?? (live ? activeSessionBlank(live) : await this.probeBlank(header.id, snapshot?.revision ?? block?.asOfSeq))
      const archived = this.ctx.get('workspaceRegistry')?.archivedSessionIds.includes(header.id) ?? false
      return sessionSummary(header, record, { title, updatedAt: valid ? value.updatedAt : header.createdAt, ...(valid ? {} : { incomplete: true }), blank, cached: !live && !!valid, archived })
    }
    snapshot ??= await persistence.stat(header.id)
    const cached = this.metadata.get(header.id)
    const seq = record?.handle?.agent.session.seq
    let metadata
    if (snapshot?.revision !== undefined && cached?.revision === snapshot.revision && cached.seq === seq) {
      metadata = cached.value
    } else {
      const handle = await persistence.open(header.id, 'read')
      metadata = { title: '', updatedAt: header.createdAt, blank: true }
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
            if (event.type === 'user/message' || event.type === 'turn/start') metadata.blank = false
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

  // A cold untitled Session without DSH's list projection is probed once per
  // revision. Reading one bounded prefix distinguishes a truly blank log from
  // a stale catalog row; titled or projected Sessions never take this path.
  async probeBlank(id, revision) {
    if (revision !== undefined) {
      const cached = this.blank.get(id)
      if (cached?.revision === revision) return cached.blank
    }
    let blank = false
    try {
      const handle = await this.ctx.get('sessionPersistence').open(id, 'read')
      try {
        const { events } = await handle.read(0, 64)
        blank = !events.some(event => event.type === 'user/message' || event.type === 'turn/start')
      } finally { await handle.close() }
    } catch { /* An unreadable cold log is not proof of blankness. */ }
    if (revision !== undefined) {
      if (this.blank.size >= 1000) this.blank.delete(this.blank.keys().next().value)
      this.blank.set(id, { revision, blank })
    }
    return blank
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
    const backward = params.direction === 'backward'
    if (params.direction !== undefined && params.direction !== 'forward' && !backward) throw fault('protocol/invalid-params', 'Invalid history direction', -32602)
    const record = this.sessions.get(id)
    if (!record?.handle) throw fault('session/not-found', 'Session is not owned by this host')
    const live = params.live === true
    if (record.modelUpdating || record.metadataUpdating || !live && (record.busy || record.handle.agent.status !== 'idle')) throw fault('session/busy', 'Wait for the session to become idle before reading history')
    const state = params.cursor === undefined ? {
      kind: 'history', id, offset: 0, nextSeq: record.handle.agent.session.seq, live,
      backward, ...(backward ? { offset: params.beforeSeq ?? record.handle.agent.session.seq } : {}),
      ...(live ? { status: record.busy || record.handle.agent.status !== 'idle' ? 'running' : 'idle', statusRevision: record.statusRevision ?? 0 } : {}),
      ...(params.assistantStream ? { assistantStream: record.assistantStream?.snapshot(this.reasoningBaseline()) ?? { revision: 0 } } : {}),
    } : this.cursor(params.cursor, 'history', id)
    if (params.beforeSeq !== undefined && (!backward || params.cursor !== undefined || !Number.isSafeInteger(params.beforeSeq) || params.beforeSeq < 0 || params.beforeSeq > state.nextSeq)) throw fault('protocol/invalid-params', 'Invalid history boundary', -32602)
    if (!!state.backward !== backward) throw fault('session/invalid-cursor', 'History cursor direction does not match')
    if (state.live !== live) throw fault('session/invalid-cursor', 'History cursor mode does not match')
    if ((state.assistantStream !== undefined) !== (params.assistantStream === true)) throw fault('session/invalid-cursor', 'History cursor stream mode does not match')
    const baselineBytes = state.assistantStream ? Buffer.byteLength(JSON.stringify(state.assistantStream)) : 0
    if (baselineBytes > Math.min(128 * 1024, this.maxPageBytes)) throw fault('session/page-too-large', 'Stream baseline exceeds the channel snapshot budget')
    const persistence = this.ctx.get('sessionPersistence')
    let events
    const start = backward ? Math.max(0, state.offset - limit) : state.offset
    const length = backward ? state.offset - start : Math.min(limit, state.nextSeq - start)
    record.historyReaders = (record.historyReaders ?? 0) + 1
    try {
      await persistence.flush()
      const handle = await persistence.open(id, 'read')
      try {
        events = (await handle.read(start, length)).events
      } finally { await handle.close() }
    } catch {
      throw fault('session/history-failed', 'Could not read stored history')
    } finally { record.historyReaders-- }
    if (events.length !== length || events.some((event, index) => event.seq !== start + index)) {
      throw fault('session/history-failed', 'Stored history does not match the captured sequence boundary')
    }
    const page = this.page(backward ? events.toReversed() : events, limit, this.maxPageBytes - baselineBytes)
    if (backward) page.reverse()
    const offset = state.offset + (backward ? -page.length : page.length)
    const hasMore = backward ? offset > 0 : offset < state.nextSeq
    return { events: page, ...(backward ? { startSeq: offset } : {}), nextSeq: state.nextSeq, hasMore, ...(live ? { status: state.status, statusRevision: state.statusRevision } : {}), ...(state.assistantStream ? { assistantStream: state.assistantStream } : {}), ...(hasMore ? { nextCursor: this.save({ ...state, offset }) } : {}) }
  }

  close() { this.cursors.clear(); this.metadata.clear(); this.blank.clear() }
}
