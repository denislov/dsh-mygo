import { Console } from 'node:console'

const STDOUT_METHODS = [
  'log', 'info', 'debug', 'dir', 'dirxml', 'table',
  'count', 'countReset', 'group', 'groupCollapsed', 'groupEnd',
  'time', 'timeLog', 'timeEnd', 'clear',
]
const INSTALLED = Symbol.for('dsh-mygo.stdout-guard')

/**
 * Keep stdout for JSON-RPC. Node sends most console diagnostics to stdout;
 * route those methods through a Console whose output goes to stderr instead.
 * warn/error/trace/assert already use stderr and stay untouched.
 * @param target console-like object to modify
 * @param sink stderr-like writable stream receiving diagnostic lines
 */
export function routeConsoleToStderr(target = console, sink = process.stderr) {
  if (target[INSTALLED] === sink) return
  const routed = new Console({ stdout: sink, stderr: sink })
  for (const method of STDOUT_METHODS) {
    if (typeof target[method] !== 'function' || typeof routed[method] !== 'function') continue
    target[method] = routed[method].bind(routed)
  }
  target[INSTALLED] = sink
}
