import { sessionID, sessionSummary } from './history.mjs'

function fault(kind, message, code = -32000) {
  return Object.assign(new Error(message), { kind, code })
}

function label(value, limit = 256, empty = false) {
  if (typeof value !== 'string' || (!empty && value.trim().length === 0) || value.length > limit || /[\u0000-\u001f\u007f]/.test(value)) throw fault('protocol/invalid-params', 'Invalid model metadata or route', -32602)
  return value
}

async function bounded(work) {
  const abort = new AbortController()
  let timer
  try {
    return await Promise.race([work(abort.signal), new Promise((_resolve, reject) => {
      timer = setTimeout(() => { abort.abort(); reject(new Error('Model operation timed out')) }, 5000)
    })])
  } finally { clearTimeout(timer) }
}

export class ModelSettings {
  constructor(server, maxBytes) { this.server = server; this.maxBytes = Math.min(256 * 1024, maxBytes) }

  result(value) {
    if (Buffer.byteLength(JSON.stringify(value)) > this.maxBytes) throw fault('model/catalog-too-large', 'Model catalog exceeds the supported budget')
    return value
  }

  record(id) {
    const record = this.server.sessions.get(sessionID(id))
    if (!record?.handle) throw fault('session/not-found', 'Session is not owned by this host')
    return record
  }

  async list(params) {
    const record = this.record(params.sessionId)
    if (record.metadataUpdating) throw fault('session/busy', 'Session metadata is being updated')
    record.catalogReaders = (record.catalogReaders ?? 0) + 1
    try { return await this.readCatalog(params, record) }
    finally { record.catalogReaders-- }
  }

  async readCatalog(params, record) {
    const provider = params.provider === undefined ? record.route.provider : label(params.provider)
    const llm = this.server.ctx.llm
    let entries
    try { entries = llm.listProviders() } catch { throw fault('model/catalog-unavailable', 'Could not read provider catalog') }
    if (!Array.isArray(entries)) throw fault('model/catalog-unavailable', 'Could not read provider catalog')
    if (entries.length > 32) throw fault('model/catalog-too-large', 'Model catalog exceeds the supported budget')
    let providers
    try {
      providers = entries.map((entry) => ({ id: label(entry.id), name: label(entry.name) }))
      if (new Set(providers.map((entry) => entry.id)).size !== providers.length) throw new Error('Duplicate provider')
    } catch {
      throw fault('model/catalog-unavailable', 'Could not read provider catalog')
    }
    const result = { providers, provider, models: [], selection: { ...record.route }, available: false }
    if (!providers.some((entry) => entry.id === provider)) return this.result({ ...result, reason: 'Provider is unavailable' })
    try {
      result.models = await bounded(async (signal) => {
        const entries = await llm.listModels(provider)
        if (entries.length > 128) throw new Error('Too many models')
        const models = []
        const seen = new Set()
        for (const entry of entries) {
          signal.throwIfAborted()
          const model = { id: label(entry.id), name: label(entry.name), efforts: [], available: true }
          if (entry.provider !== provider || seen.has(model.id)) throw new Error('Invalid model catalog')
          seen.add(model.id)
          try {
            const metadata = await llm.resolveModelInfo(provider, model.id, signal)
            signal.throwIfAborted()
            const efforts = metadata.reasoning?.efforts ?? []
            if (efforts.length > 32) throw new Error('Too many efforts')
            model.efforts = efforts.map((effort) => ({ id: label(effort.id), name: label(effort.name) }))
            if (new Set(model.efforts.map((effort) => effort.id)).size !== model.efforts.length) throw new Error('Duplicate effort')
            if (metadata.inputModalities !== undefined) {
              const modalities = metadata.inputModalities
              if (!Array.isArray(modalities) || modalities.length > 8 || modalities.some((value) => !['text', 'image'].includes(value))) throw new Error('Invalid model modalities')
              model.inputModalities = [...new Set(modalities)]
            }
          } catch {
            if (signal.aborted) throw new Error('Cancelled catalog')
            model.efforts = []
            model.available = false
            model.reason = 'Model metadata is unavailable'
          }
          models.push(model)
        }
        if (Buffer.byteLength(JSON.stringify(models)) > 256 * 1024) throw new Error('Catalog too large')
        return models
      })
      result.available = true
      return this.result(result)
    } catch {
      return this.result({ ...result, available: false, models: [], reason: 'Model catalog is unavailable; a manual model ID can still be validated' })
    }
  }

  async select(params) {
    const record = this.record(params.sessionId)
    const provider = label(params.provider)
    const model = label(params.model)
    const effort = params.effort === undefined ? '' : label(params.effort, 256, true)
    if (record.busy || record.modelUpdating || record.metadataUpdating || record.handle.agent.status !== 'idle') throw fault('session/busy', 'Wait for the session to become idle')
    record.modelUpdating = true
    try {
      const selected = { provider, model, ...(effort === '' ? {} : { reasoningEffort: this.server.reasoningEffort(effort) }) }
      await bounded((signal) => this.server.ctx.llm.resolveCallConfig(selected, signal))
      if (this.server.stopping || record.busy || record.handle.agent.status !== 'idle') throw new Error('Session changed')
      record.handle.agent.session.append('model/selection', selected)
      record.selection.current = selected
      record.route = { provider, model, effort }
      return { session: sessionSummary(record.handle.agent.session.header, record) }
    } catch {
      throw fault('model/select-failed', 'Could not select the model; refresh to confirm the active route before retrying')
    } finally { record.modelUpdating = false }
  }
}
