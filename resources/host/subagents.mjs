// Read-only child discovery and history over DSH's public services. A child is
// never inserted into MygoServer.sessions or resumed for inspection.
class InspectionFault extends Error {
  constructor(kind, message, code) { super(message); this.kind = kind; this.code = code }
}
function fault(kind, message, code = -32000) { return new InspectionFault(kind, message, code) }
function identifier(value) {
  if (typeof value !== 'string' || !/^[a-zA-Z0-9_.:-]{1,128}$/.test(value)) throw fault('protocol/invalid-params', 'Invalid subagent address', -32602)
  return value
}
const modes = new Set(['one-shot', 'continuable', 'external', 'unknown'])
const reasons = new Set(['completed', 'aborted', 'error', 'max-tokens', 'refusal'])

// Strip model-hidden metadata, request configuration, reasoning, stream copies
// and arbitrary provider fields before crossing the process boundary. Hidden
// events retain only their offsets, so pages remain contiguous and verifiable.
function displayEvent(event) {
  const textContent = content => Array.isArray(content) ? content.filter(b => b?.type === 'text' && typeof b.text === 'string').map(b => ({ type: 'text', text: b.text })) : []
  const data = event.data
  let safe, type = event.type
  switch (type) {
    case 'turn/start': safe = {}; break
    case 'assistant/message':
      safe = { message: { content: textContent(data.message?.content) }, ...(data.interrupted === true ? { interrupted: true } : {}) }
      break
    case 'user/message':
      if (data.source?.kind === 'user') safe = { source: { kind: 'user' }, content: textContent(data.content) }
      break
    case 'tool/call':
      if (typeof data.callId === 'string' && typeof data.name === 'string' && typeof data.arguments === 'string') safe = { callId: data.callId, name: data.name, arguments: data.arguments }
      break
    case 'tool/result':
      if (typeof data.message?.toolCallId === 'string') safe = { message: { toolCallId: data.message.toolCallId, content: textContent(data.message.content) }, ...(typeof data.error?.reason === 'string' ? { error: { reason: data.error.reason } } : {}) }
      break
    case 'turn/end': {
      const kind = data.reason?.kind
      safe = { reason: { kind: reasons.has(kind) ? kind : 'error' } }
      break
    }
  }
  if (safe === undefined) { type = 'mygo/hidden'; safe = {} }
  return { seq: event.seq, time: event.time, type, data: safe }
}

export class SubagentInspection {
  constructor(server, maxFrameBytes) {
    this.server = server
    this.maxBytes = Math.min(256 * 1024, Math.floor(maxFrameBytes / 2))
    this.pending = new Set()
    this.abort = new AbortController()
  }

  supported() {
    return typeof this.server.ctx.get('subagents')?.listChildren === 'function' && typeof this.server.ctx.get('sessionQuery')?.observeSession === 'function'
  }

  run(params, read) {
    const parentSessionId = identifier(params.parentSessionId)
    const record = this.server.sessions.get(parentSessionId)
    if (!record?.handle || record.metadataUpdating || record.releaseTask || this.server.stopping) throw fault('subagent/unavailable', 'The parent session is unavailable')
    if (this.pending.size >= 8) throw fault('subagent/busy', 'Too many subagent reads are pending')
    record.catalogReaders = (record.catalogReaders ?? 0) + 1
    const signal = AbortSignal.any([this.abort.signal, AbortSignal.timeout(15000)])
    const task = (async () => {
      try {
        const entries = await this.server.ctx.get('subagents').listChildren(parentSessionId, signal)
        signal.throwIfAborted()
        if (!Array.isArray(entries) || entries.length > 128) throw fault('subagent/catalog-too-large', 'The subagent catalog exceeds the supported size')
        const seen = new Set()
        for (const entry of entries) {
          identifier(entry?.id)
          if (seen.has(entry.id) || !modes.has(entry.mode) || !Number.isSafeInteger(entry.createdAt) || entry.createdAt < 0 || entry.label !== undefined && (typeof entry.label !== 'string' || Buffer.byteLength(entry.label) > 1024 || /[\u0000-\u001f\u007f]/.test(entry.label))) throw fault('subagent/read-failed', 'Could not read subagent information')
          seen.add(entry.id)
        }
        const result = await read(entries, signal, parentSessionId)
        signal.throwIfAborted()
        if (Buffer.byteLength(JSON.stringify(result)) > this.maxBytes) throw fault('subagent/page-too-large', 'The subagent page exceeds the supported size')
        return result
      } catch (error) {
        if (error instanceof InspectionFault) throw error
        throw fault('subagent/read-failed', 'Could not read subagent information. Refresh to retry')
      } finally { record.catalogReaders-- }
    })()
    this.pending.add(task)
    return task.finally(() => this.pending.delete(task))
  }

  status(id, mode) {
    if (mode === 'external') return 'external'
    const status = this.server.ctx.get('agents')?.get?.(id)?.status
    return status === 'running' || status === 'idle' ? status : 'stored'
  }

  list(params) {
    return this.run(params, async (entries, _signal, parentSessionId) => ({
      parentSessionId, children: entries.map(entry => ({ id: entry.id, mode: entry.mode, label: entry.label ?? '', createdAt: entry.createdAt, status: this.status(entry.id, entry.mode) })),
    }))
  }

  history(params) {
    const childSessionId = identifier(params.childSessionId)
    if (params.beforeSeq !== undefined && (!Number.isSafeInteger(params.beforeSeq) || params.beforeSeq < 0)) throw fault('protocol/invalid-params', 'Invalid child history boundary', -32602)
    return this.run(params, async (entries, signal, parentSessionId) => {
      const entry = entries.find(e => e.id === childSessionId)
      if (!entry) throw fault('subagent/not-found', 'The subagent does not belong to this parent')
      if (entry.mode === 'external' || entry.mode === 'unknown') throw fault('subagent/unavailable', 'No local history is available for this subagent')
      const observed = await this.server.ctx.get('sessionQuery').observeSession(childSessionId, { signal, projectionMode: 'none' })
      try {
        signal.throwIfAborted()
        if (observed.header?.id !== childSessionId || observed.header.parentSession !== parentSessionId || observed.header.origin !== 'subagent') throw fault('subagent/unavailable', 'The child session lineage is unavailable')
        const inheritedEventCount = observed.inheritedEventCount
        const nextSeq = observed.cursor + 1
        if (!Number.isSafeInteger(nextSeq) || !Number.isSafeInteger(inheritedEventCount) || inheritedEventCount < 0 || nextSeq < inheritedEventCount) throw new Error('Invalid child observation')
        const endSeq = params.beforeSeq ?? nextSeq
        if (endSeq < inheritedEventCount || endSeq > nextSeq) throw fault('protocol/invalid-params', 'Invalid child history boundary', -32602)
        let startSeq = Math.max(inheritedEventCount, endSeq - 64)
        let events = observed.events.slice(startSeq, endSeq)
        if (events.length !== endSeq - startSeq || events.some((e, i) => e.seq !== startSeq + i || !Number.isSafeInteger(e.time) || e.time < 0 || !e.data || typeof e.data !== 'object')) throw new Error('Invalid child events')
        events = events.map(displayEvent)
        // Keep the newest contiguous suffix that fits. A single oversized
        // event fails explicitly rather than masquerading as complete output.
        while (events.length > 1 && Buffer.byteLength(JSON.stringify(events)) > this.maxBytes - 4096) { events.shift(); startSeq++ }
        return { parentSessionId, childSessionId, startSeq, endSeq, nextSeq, inheritedEventCount, hasMore: startSeq > inheritedEventCount, events }
      } finally { observed[Symbol.dispose]() }
    })
  }

  async close() {
    this.abort.abort()
    await Promise.allSettled([...this.pending])
  }
}
