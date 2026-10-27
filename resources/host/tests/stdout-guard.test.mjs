import assert from 'node:assert/strict'
import { Writable } from 'node:stream'
import { test } from 'node:test'
import { routeConsoleToStderr } from '../stdout-guard.mjs'

function sink() {
  const chunks = []
  const stream = new Writable({
    write(chunk, _encoding, callback) {
      chunks.push(chunk.toString())
      callback()
    },
  })
  return { stream, text: () => chunks.join('') }
}

test('console stdout methods move to stderr without touching warn or error', () => {
  const output = sink()
  const stdoutCalls = []
  const target = {
    log: (...args) => { stdoutCalls.push(args) },
    info: (...args) => { stdoutCalls.push(args) },
    debug: (...args) => { stdoutCalls.push(args) },
    dir: (...args) => { stdoutCalls.push(args) },
    table: (...args) => { stdoutCalls.push(args) },
    count: (...args) => { stdoutCalls.push(args) },
    countReset: (...args) => { stdoutCalls.push(args) },
    group: (...args) => { stdoutCalls.push(args) },
    groupEnd: (...args) => { stdoutCalls.push(args) },
    time: (...args) => { stdoutCalls.push(args) },
    timeEnd: (...args) => { stdoutCalls.push(args) },
    warn: () => {},
    error: () => {},
    trace: () => {},
    assert: () => {},
  }
  const warn = target.warn
  const error = target.error
  const trace = target.trace
  const assertMethod = target.assert
  routeConsoleToStderr(target, output.stream)
  target.log('loaded', 2)
  target.info('model %s', 'stdout-probe')
  target.debug({ model: 'stdout-probe' })
  target.dir('value')
  target.table([{ model: 'stdout-probe' }])
  target.count('count-probe')
  target.group('group-probe')
  target.log('grouped')
  target.groupEnd()
  target.time('time-probe')
  target.timeEnd('time-probe')
  assert.equal(stdoutCalls.length, 0)
  const text = output.text()
  assert.match(text, /^loaded 2\nmodel stdout-probe\n/m)
  assert.match(text, /stdout-probe/)
  assert.match(text, /value/)
  assert.match(text, /count-probe: 1/)
  assert.match(text, /group-probe/)
  assert.match(text, /time-probe:/)
  assert.equal(target.warn, warn)
  assert.equal(target.error, error)
  assert.equal(target.trace, trace)
  assert.equal(target.assert, assertMethod)

  const installed = target.log
  routeConsoleToStderr(target, output.stream)
  assert.equal(target.log, installed)
  target.log('again')
  assert.match(output.text(), /again\n$/)
})
