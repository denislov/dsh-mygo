import assert from 'node:assert/strict'
import { test } from 'node:test'
import { QuestionQueue } from '../question.mjs'

const questions = [
  { id: 'mode', question: 'Choose mode', options: [{ label: 'Safe', description: 'Recommended' }, { label: 'Fast' }] },
  { id: 'features', question: 'Choose features', multiSelect: true, options: [{ label: 'Logs' }, { label: 'Tests' }] },
  { id: 'note', question: 'Add a note', detail: 'Full supporting detail' },
]
const answers = [{ id: 'mode', selected: ['Safe'] }, { id: 'features', selected: ['Logs', 'Tests'], custom: 'Extra' }, { id: 'note', selected: [], custom: 'Hello' }]
function fixture(maxPending = 32) {
  const notifications = []
  return { queue: new QuestionQueue({ maxPending, notify: (method, params) => notifications.push({ method, params }) }), notifications }
}

test('questions preserve the full batch and validate all answers before settling', async () => {
  const { queue, notifications } = fixture()
  const waiting = queue.ask({ questions }, 's1')
  const { requestId } = notifications[0].params
  assert.deepEqual(notifications[0].params.questions, questions)
  for (const invalid of [[], answers.slice(1), [{ id: 'mode', selected: ['Unknown'] }, ...answers.slice(1)], [{ id: 'mode', selected: ['Safe', 'Fast'] }, ...answers.slice(1)], [{ id: 'mode', selected: ['Safe'], custom: 'Both' }, ...answers.slice(1)], [answers[0], answers[0], answers[2]], [answers[0], { id: 'features', selected: ['Logs', 'Logs'] }, answers[2]], [...answers.slice(0, 2), { id: 'note', selected: [], custom: ' ' }]]) {
    assert.throws(() => queue.respond({ requestId, answers: invalid }), { kind: 'protocol/invalid-params' })
  }
  assert.deepEqual(queue.respond({ requestId, answers }), {})
  assert.deepEqual(await waiting, { answers })
  assert.equal(notifications.at(-1).method, 'question/closed')
  assert.throws(() => queue.respond({ requestId, answers }), { kind: 'question/stale-request' })
})

test('explicit cancellation, abort, session cancellation and shutdown reject instead of fabricating answers', async () => {
  const { queue, notifications } = fixture()
  const waiting = queue.ask({ questions }, '')
  const cancelled = assert.rejects(waiting, { name: 'UserQuestionError', code: 'ASK_CANCELLED' })
  queue.cancel({ requestId: notifications[0].params.requestId })
  await cancelled
  const abort = new AbortController()
  const aborted = assert.rejects(queue.ask({ questions, signal: abort.signal }, 's1'), { code: 'ASK_ABORTED' })
  abort.abort()
  await aborted
  const session = assert.rejects(queue.ask({ questions }, 's1'), { code: 'ASK_CANCELLED' })
  const global = assert.rejects(queue.ask({ questions }, ''), { code: 'ASK_CANCELLED' })
  queue.cancelSession('s1')
  await session
  assert.equal(queue.pending.size, 1)
  queue.close()
  await global
  await assert.rejects(queue.ask({ questions }, ''), { code: 'ASK_CANCELLED' })
})

test('question limits and unsupported timed waits fail closed without leaking content', async () => {
  const { queue } = fixture(1)
  await assert.rejects(queue.ask({ questions: [] }, 's1'), { code: 'BAD_REQUEST' })
  await assert.rejects(queue.ask({ questions: [questions[0], questions[0]] }, 's1'), { code: 'BAD_REQUEST' })
  await assert.rejects(queue.ask({ questions: [{ id: 'q', question: 'secret'.repeat(20000) }] }, 's1'), (error) => error.code === 'BAD_REQUEST' && !error.message.includes('secret'))
  await assert.rejects(queue.ask({ questions, wait: { timed: true } }, 's1'), { code: 'NO_PROVIDER' })
  const pending = assert.rejects(queue.ask({ questions }, 's1'), { code: 'ASK_CANCELLED' })
  await assert.rejects(queue.ask({ questions }, 's1'), { code: 'NO_PROVIDER' })
  queue.close()
  await pending
})
