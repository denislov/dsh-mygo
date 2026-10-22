import assert from 'node:assert/strict'
import { test } from 'node:test'
import { ApprovalQueue } from '../approval.mjs'

function queue(maxPending = 32) {
  const notifications = []
  return { approvals: new ApprovalQueue({ maxPending, notify: (method, params) => notifications.push({ method, params }) }), notifications }
}

const request = { toolName: 'write', callId: 'call-1', reason: 'Write a file outside the workspace' }
const owner = { sessionId: 's1', call: { name: 'write', arguments: '{"file_path":"outside.txt","content":"hello"}' } }

test('approval uses unique identities and maps only one-shot decisions', async () => {
  const { approvals, notifications } = queue()
  const waiting = approvals.ask(request, owner)
  const pending = notifications[0].params
  assert.equal(notifications[0].method, 'approval/request')
  assert.equal(pending.external, false)
  assert.equal(pending.arguments, owner.call.arguments)
  assert.throws(() => approvals.respond({ requestId: pending.requestId, decision: 'allow-always' }), { code: -32602 })
  assert.throws(() => approvals.respond({ requestId: pending.requestId, decision: 'allow', remember: true }), { code: -32602 })
  assert.deepEqual(approvals.respond({ requestId: pending.requestId, decision: 'allow' }), {})
  assert.equal(await waiting, 'allowed-once')
  assert.equal(notifications.at(-1).method, 'approval/closed')
  assert.throws(() => approvals.respond({ requestId: pending.requestId, decision: 'deny' }), { kind: 'approval/stale-request' })
  const denied = approvals.ask(request, owner)
  const next = notifications.at(-1).params
  assert.notEqual(next.requestId, pending.requestId)
  approvals.respond({ requestId: next.requestId, decision: 'deny' })
  assert.equal(await denied, 'rejected')
})

test('parallel duplicate tool requests are external and answered independently', async () => {
  const { approvals, notifications } = queue()
  const first = approvals.ask(request, owner)
  const second = approvals.ask(request, owner)
  assert.equal(notifications[0].params.external, false)
  assert.equal(notifications[1].params.external, true)
  approvals.respond({ requestId: notifications[1].params.requestId, decision: 'deny' })
  assert.equal(await second, 'rejected')
  approvals.respond({ requestId: notifications[0].params.requestId, decision: 'allow' })
  assert.equal(await first, 'allowed-once')
})

test('abort, session cancellation and shutdown withdraw all waits', async () => {
  const { approvals, notifications } = queue()
  const abort = new AbortController()
  const first = approvals.ask({ ...request, signal: abort.signal }, owner)
  const firstID = notifications[0].params.requestId
  abort.abort()
  assert.equal(await first, 'cancelled')
  assert.throws(() => approvals.respond({ requestId: firstID, decision: 'allow' }), { kind: 'approval/stale-request' })
  const second = approvals.ask(request, owner)
  const third = approvals.ask(request, { sessionId: 's2' })
  approvals.cancelSession('s1')
  assert.equal(await second, 'cancelled')
  approvals.close()
  assert.equal(await third, 'cancelled')
  assert.equal(await approvals.ask(request, owner), 'cancelled')
})

test('limits fail closed without truncating commands or leaking request text in errors', async () => {
  const { approvals, notifications } = queue(1)
  const waiting = approvals.ask(request, owner)
  assert.equal(await approvals.ask(request, owner), 'unavailable')
  approvals.cancelSession('s1')
  await waiting
  assert.equal(await approvals.ask(request, { ...owner, call: { name: 'write', arguments: 'secret'.repeat(20000) } }), 'unavailable')
  const abort = new AbortController()
  abort.abort()
  assert.equal(await approvals.ask({ ...request, signal: abort.signal }, owner), 'cancelled')
  assert.equal(notifications.filter((entry) => entry.method === 'approval/request').length, 1)
  assert.throws(() => approvals.respond({ requestId: 'private-token', decision: 'allow' }), (error) => error.kind === 'approval/stale-request' && !error.message.includes('private-token'))
})
