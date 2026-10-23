import { sessionID } from './history.mjs'

function fault(kind, message, code = -32000) {
  return Object.assign(new Error(message), { kind, code })
}

// All mutations stay reserved until the underlying durability barrier settles.
export class SessionManagement {
  constructor(server) { this.server = server; this.pending = new Map() }

  rename(params) {
    const id = sessionID(params.sessionId)
    const title = params.title
    if (typeof title !== 'string' || !title.trim() || Buffer.byteLength(title) > 1024 || /[\u0000-\u001f\u007f]/.test(title)) {
      throw fault('protocol/invalid-params', 'Invalid session title', -32602)
    }
    return this.mutate(id, 'rename', async (record) => {
      if (!record?.handle) throw fault('session/not-found', 'Resume the session before renaming it')
      this.server.ctx.get('sessionTitle').rename(record.handle.agent.session, title)
      await this.server.ctx.get('sessionPersistence').flush()
      await this.server.ctx.get('sessionProjectionCache')?.write?.(record.handle.agent.session).catch(() => {})
      return { session: await this.server.history.describe(record.handle.agent.session.header, record) }
    })
  }

  archive(params) {
    const id = sessionID(params.sessionId)
    if (params.archived !== undefined && typeof params.archived !== 'boolean') throw fault('protocol/invalid-params', 'Invalid archived flag', -32602)
    return this.mutate(id, 'archive', async () => {
      const snapshot = await this.server.ctx.get('sessionPersistence').stat(id)
      if (!snapshot) throw fault('session/not-found', 'Session was not found')
      if (snapshot.header.origin === 'subagent' || snapshot.formatStatus === 'migration-required') throw fault('session/unavailable', 'This session cannot be archived')
      const registry = this.server.ctx.get('workspaceRegistry')
      if (params.archived === false) await registry.unarchiveSession(id)
      else await registry.archiveSession(id)
      return {}
    })
  }

  mutate(id, operation, work) {
    const record = this.server.sessions.get(id)
    if (this.pending.has(id) || record && (!record.handle || record.busy || record.modelUpdating || record.metadataUpdating || record.handle.agent.status !== 'idle')) {
      throw fault('session/busy', 'Wait for the session to become idle')
    }
    if (record) record.metadataUpdating = true
    // Queue work after publishing the reservation, including synchronous rename.
    const task = Promise.resolve().then(() => work(record)).catch((error) => {
      if (['session/not-found', 'session/unavailable'].includes(error.kind)) throw error
      throw fault(`session/${operation}-failed`, `Could not ${operation} the session; refresh before retrying`)
    }).finally(() => {
      if (record) record.metadataUpdating = false
      this.pending.delete(id)
    })
    this.pending.set(id, task)
    return task
  }

  async drain() { await Promise.allSettled([...this.pending.values()]) }
}
