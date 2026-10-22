function fault(kind, message, code = -32000) {
  return Object.assign(new Error(message), { kind, code })
}

function label(value, empty = false) {
  if (typeof value !== 'string' || Buffer.byteLength(value) > 256 || (!empty && !value.trim()) || /[\u0000-\u001f\u007f]/.test(value)) throw fault('protocol/invalid-params', 'Invalid configuration field', -32602)
  return value
}

function route(value) {
  return { provider: label(value.provider), model: label(value.model), effort: label(value.reasoningEffort ?? '', true) }
}

export class Configuration {
  constructor(server, credentialRef) {
    this.server = server
    this.credentialRef = credentialRef
    this.pending = undefined
    this.uncertain = false
  }

  supported() {
    const ctx = this.server.ctx
    return typeof this.credentialRef === 'function' && typeof ctx.get('agentDefaultModel')?.currentSelection === 'function' && ['describe', 'replace'].every((method) => typeof ctx.get('settings')?.[method] === 'function') && ['describe', 'set', 'unset'].every((method) => typeof ctx.get('credentials')?.[method] === 'function') && typeof ctx.llm.listConfigurableProviders === 'function'
  }

  descriptors() { return this.server.ctx.get('settings').describe({ redactSecrets: true }) }

  defaults() {
    const descriptor = this.descriptors().find((entry) => entry.ns === 'agent-default-model')
    const selection = route(this.server.ctx.get('agentDefaultModel').currentSelection())
    if (!descriptor || !Number.isSafeInteger(descriptor.revision) || descriptor.revision < 0 || JSON.stringify(route(descriptor.value)) !== JSON.stringify(selection)) throw new Error('Default configuration is unavailable')
    return { defaults: selection, revision: descriptor.revision }
  }

  reference() {
    const provider = this.server.ctx.llm.listConfigurableProviders().find((entry) => entry.provider === 'deepseek-official')
    if (!provider || provider.settingsNs !== 'llm-deepseek' || provider.settingsPath.length !== 0) return undefined
    const descriptor = this.descriptors().find((entry) => entry.ns === provider.settingsNs)
    if (!descriptor || typeof descriptor.value?.apiKeyEnv !== 'string') return undefined
    const reference = label(descriptor.value.apiKeyEnv)
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(reference)) return undefined
    return reference
  }

  async snapshot() {
    const defaults = this.defaults()
    const reference = this.reference()
    let credential = { available: false, reference: '', configured: false, writable: false, source: 'none' }
    if (reference !== undefined) {
      const info = await this.server.ctx.get('credentials').describe(this.credentialRef(reference))
      if (typeof info?.configured !== 'boolean' || typeof info.writable !== 'boolean') throw new Error('Credential metadata is unavailable')
      credential = { available: true, reference, configured: info.configured, writable: info.writable, source: !info.configured ? 'none' : info.source === 'file' ? 'managed' : info.source === 'env' ? 'environment' : 'fallback' }
    }
    return { ...defaults, credential }
  }

  async get() {
    if (this.pending) await this.pending.catch(() => {})
    try {
      const snapshot = await this.snapshot()
      this.uncertain = false
      return snapshot
    } catch { throw fault('settings/unavailable', 'Could not read configuration; check the Host profile') }
  }

  assertIdle() {
    if (this.pending) throw fault('settings/busy', 'A configuration write is still pending')
    if (this.uncertain) throw fault('settings/refresh-required', 'Refresh configuration before retrying')
    if ([...this.server.sessions.values()].some((record) => !record.handle || record.busy || record.modelUpdating || record.handle.agent.status !== 'idle')) throw fault('session/busy', 'Wait for all sessions to become idle')
  }

  write(work) {
    this.assertIdle()
    const task = Promise.resolve().then(work)
    this.pending = task
    return task.catch((error) => {
      this.uncertain = true
      if (error.code === 'SETTINGS_CONFLICT' || error.kind === 'settings/conflict') throw fault('settings/conflict', 'Configuration changed; refresh before retrying')
      throw fault('settings/write-failed', 'Could not confirm the configuration write; refresh before retrying')
    }).finally(() => { this.pending = undefined })
  }

  saveDefaults(params) {
    const provider = label(params.provider)
    const model = label(params.model)
    const effort = label(params.effort ?? '', true)
    if (!Number.isSafeInteger(params.revision) || params.revision < 0) throw fault('protocol/invalid-params', 'Invalid configuration revision', -32602)
    return this.write(async () => {
      if (this.defaults().revision !== params.revision) throw fault('settings/conflict', 'Configuration changed')
      await this.server.ensureProvider?.(provider)
      const abort = new AbortController()
      const timer = setTimeout(() => abort.abort(), 5000)
      try {
        await this.server.ctx.llm.resolveCallConfig({ provider, model, ...(effort ? { reasoningEffort: this.server.reasoningEffort(effort) } : {}) }, abort.signal)
        abort.signal.throwIfAborted()
      } finally { clearTimeout(timer) }
      if (this.server.stopping) throw new Error('Host is stopping')
      await this.server.ctx.get('settings').replace('agent-default-model', { provider, model, ...(effort ? { reasoningEffort: effort } : {}) }, params.revision)
      const snapshot = await this.snapshot()
      if (JSON.stringify(snapshot.defaults) !== JSON.stringify({ provider, model, effort })) throw new Error('Default write was not applied')
      return snapshot
    })
  }

  saveCredential(params) {
    const reference = label(params.reference)
    if (!['set', 'unset'].includes(params.operation) || params.operation === 'unset' && params.value !== undefined) throw fault('protocol/invalid-params', 'Invalid credential operation', -32602)
    if (params.operation === 'set' && (typeof params.value !== 'string' || !params.value.trim() || Buffer.byteLength(params.value) > 8192 || /[\u0000-\u0020\u007f]/.test(params.value))) throw fault('protocol/invalid-params', 'Invalid credential input', -32602)
    const value = params.value
    return this.write(async () => {
      if (this.reference() !== reference) throw fault('settings/conflict', 'Credential binding changed')
      const credentials = this.server.ctx.get('credentials')
      const ref = this.credentialRef(reference)
      if ((await credentials.describe(ref))?.writable !== true || this.server.stopping) throw new Error('Credential is not writable')
      if (params.operation === 'set') await credentials.set(ref, value)
      else await credentials.unset(ref)
      if (this.reference() !== reference) throw fault('settings/conflict', 'Credential binding changed')
      const snapshot = await this.snapshot()
      if (params.operation === 'set' && !snapshot.credential.configured) throw new Error('Credential write was not applied')
      return snapshot
    })
  }

  sessionRoute(params) {
    if (params.useDefaultModel === undefined) return params
    if (params.useDefaultModel !== true || ['provider', 'model', 'effort'].some((field) => params[field] !== undefined)) throw fault('protocol/invalid-params', 'Default routing cannot be combined with an explicit route', -32602)
    if (!this.server.capabilities.includes('settings.configuration')) throw fault('protocol/capability-unavailable', 'Configuration was not negotiated')
    if (this.pending || this.uncertain) throw fault('settings/refresh-required', 'Refresh configuration before creating a session')
    try {
      const selected = this.defaults().defaults
      return { ...params, provider: selected.provider, model: selected.model, ...(selected.effort ? { effort: selected.effort } : {}) }
    } catch { throw fault('settings/unavailable', 'Could not read the default model') }
  }
}
