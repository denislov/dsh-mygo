import assert from 'node:assert/strict'
import { test } from 'node:test'
import { AssistantStreamBaseline } from '../stream-baseline.mjs'

test('baseline restores text and chunk position without retaining private blocks', () => {
  const stream = new AssistantStreamBaseline()
  stream.accept({ type: 'start', revision: 1, attemptId: 's:1', turn: 1, step: 2 })
  stream.accept({ type: 'chunk', revision: 2, attemptId: 's:1', index: 0, chunk: { type: 'block-start', index: 0, blockType: 'text' } })
  stream.accept({ type: 'chunk', revision: 3, attemptId: 's:1', index: 1, chunk: { type: 'text-delta', index: 0, text: '你好' } })
  const first = stream.snapshot()
  assert.equal(first.activeAttempt.nextIndex, 2)
  assert.equal(first.activeAttempt.blocks[0].text, '你好')
  stream.accept({ type: 'chunk', revision: 4, attemptId: 's:1', index: 2, chunk: { type: 'reasoning-delta', index: 1, text: 'private-reasoning' } })
  assert.equal(first.revision, 3)
  assert.ok(!JSON.stringify(stream.snapshot()).includes('private-reasoning'))
  stream.commit({ type: 'assistant/message', data: { turn: 1, step: 2 } })
  assert.deepEqual(stream.snapshot(), { revision: 4 })
  stream.accept({ type: 'end', revision: 5, attemptId: 's:1', index: 3 })
  assert.deepEqual(stream.snapshot(), { revision: 5 })
})

test('baseline bounds text and blocks and discards broken attempts until a new start', () => {
  const stream = new AssistantStreamBaseline()
  stream.accept({ type: 'start', revision: 1, attemptId: 's:1', turn: 1, step: 1 })
  stream.accept({ type: 'chunk', revision: 2, attemptId: 's:1', index: 0, chunk: { type: 'block-start', index: 0, blockType: 'text' } })
  stream.accept({ type: 'chunk', revision: 3, attemptId: 's:1', index: 1, chunk: { type: 'text-delta', index: 0, text: '😀'.repeat(9000) } })
  assert.equal([...stream.snapshot().activeAttempt.blocks[0].text].length, 8000)
  assert.equal(stream.snapshot().activeAttempt.truncated, true)
  stream.accept({ type: 'chunk', revision: 5, attemptId: 's:1', index: 2, chunk: { type: 'text-delta', index: 0, text: 'gap' } })
  assert.deepEqual(stream.snapshot(), { revision: 5 })
  stream.accept({ type: 'start', revision: 6, attemptId: 's:2', turn: 1, step: 1 })
  for (let index = 0; index < 129; index++) stream.accept({ type: 'chunk', revision: 7 + index, attemptId: 's:2', index, chunk: { type: 'block-start', index, blockType: 'reasoning' } })
  assert.equal(stream.snapshot().activeAttempt, undefined)
})
