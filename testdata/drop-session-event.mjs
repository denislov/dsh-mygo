// Fault-injection transport used only by integration tests. The durable Host
// log stays intact while one durable or stream notification is lost before Go.
import { spawn } from 'node:child_process'
import { createInterface } from 'node:readline'

const argumentsList = process.argv.slice(2)
const streamGap = argumentsList[0] === '--stream-gap'
if (streamGap) argumentsList.shift()
const [command, ...args] = argumentsList
const child = spawn(command, args, { stdio: ['pipe', 'pipe', 'inherit'], windowsHide: true })
process.stdin.pipe(child.stdin)
child.stdin.on('error', () => {})
let dropped = false
const lines = createInterface({ input: child.stdout })
lines.on('line', (line) => {
  try {
    const frame = JSON.parse(line)
    const missing = streamGap
      ? frame.method === 'session/assistant-stream' && frame.params?.frame?.chunk?.type === 'block-start' && frame.params.frame.chunk.blockType === 'text'
      : frame.method === 'session/event' && frame.params?.event?.type === 'user/message'
    if (!dropped && missing) {
      dropped = true
      return
    }
    if (!process.stdout.write(`${line}\n`)) lines.pause()
  } catch { child.kill(); process.exitCode = 1 }
})
process.stdout.on('drain', () => lines.resume())
child.on('error', () => { process.stdin.destroy(); process.exitCode = 1 })
child.on('close', (code) => { process.stdin.destroy(); process.exitCode = code ?? 1 })
