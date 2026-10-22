import { randomUUID } from 'node:crypto'

function fault(kind, message, code = -32000) {
  return Object.assign(new Error(message), { kind, code })
}

function validText(value, limit) {
  return typeof value === 'string' && value.length > 0 && Buffer.byteLength(value) <= limit && !value.includes('\0')
}

export class ApprovalQueue {
  constructor({ notify, maxPending = 32 }) {
    this.notify = notify
    this.maxPending = maxPending
    this.pending = new Map()
    this.closed = false
  }

  ask(request, { sessionId, call }) {
    if (this.closed || request.signal?.aborted) return Promise.resolve('cancelled')
    if (this.pending.size >= this.maxPending || !validText(request.toolName, 256)
      || request.reason !== undefined && !validText(request.reason, 4096)
      || request.callId !== undefined && !validText(request.callId, 256)
      || call && (!validText(call.arguments, 65536) || !validText(call.name, 256))) return Promise.resolve('unavailable')
    const duplicate = request.callId !== undefined && [...this.pending.values()].some((entry) => entry.sessionId === sessionId && entry.callId === request.callId)
    const external = !call || call.name !== request.toolName || duplicate
    const requestId = randomUUID()
    return new Promise((resolve) => {
      const entry = {
        requestId, sessionId, callId: request.callId, resolve, signal: request.signal,
        onAbort: () => this.settle(requestId, 'cancelled'),
      }
      this.pending.set(requestId, entry)
      entry.signal?.addEventListener('abort', entry.onAbort, { once: true })
      if (entry.signal?.aborted) { this.settle(requestId, 'cancelled'); return }
      this.notify('approval/request', {
        requestId, sessionId, agentId: sessionId, toolName: request.toolName,
        reason: request.reason ?? '', external,
        ...(request.callId === undefined ? {} : { callId: request.callId }),
        ...(external ? {} : { arguments: call.arguments }),
      })
    })
  }

  respond(params) {
    if (typeof params.requestId !== 'string' || !/^[a-zA-Z0-9_-]{1,128}$/.test(params.requestId)
      || !['allow', 'deny'].includes(params.decision) || Object.hasOwn(params, 'remember')) {
      throw fault('protocol/invalid-params', 'Invalid approval response', -32602)
    }
    if (!this.pending.has(params.requestId)) throw fault('approval/stale-request', 'The approval request is no longer pending')
    this.settle(params.requestId, params.decision === 'allow' ? 'allowed-once' : 'rejected')
    return {}
  }

  settle(requestId, outcome) {
    const entry = this.pending.get(requestId)
    if (!entry) return
    this.pending.delete(requestId)
    entry.signal?.removeEventListener('abort', entry.onAbort)
    this.notify('approval/closed', { requestId, sessionId: entry.sessionId })
    entry.resolve(outcome)
  }

  cancelSession(sessionId) {
    for (const entry of this.pending.values()) {
      if (entry.sessionId === sessionId) this.settle(entry.requestId, 'cancelled')
    }
  }

  close() {
    this.closed = true
    for (const entry of this.pending.values()) this.settle(entry.requestId, 'cancelled')
  }
}
