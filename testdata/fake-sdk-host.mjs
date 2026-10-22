// A minimal SDK-protocol host for P0/P1 smoke runs when no DSH runtime is
// installed. It speaks newline-delimited JSON-RPC on stdio and emits one
// committed turn per prompt. It is test data, not a product host.
import readline from 'node:readline'

const lines = readline.createInterface({ input: process.stdin, crlfDelay: Infinity })

function write(message) {
  process.stdout.write(`${JSON.stringify(message)}\n`)
}

function respond(id, result) {
  write({ jsonrpc: '2.0', id, result })
}

function notify(method, params) {
  write({ jsonrpc: '2.0', method, params })
}

function fail(id, message) {
  write({ jsonrpc: '2.0', id, error: { code: -32000, message } })
}

let turn = Promise.resolve()
let seq = 0

function promptText(params) {
  const blocks = params?.contentBlocks ?? []
  return blocks
    .filter((block) => block?.type === 'text' && typeof block.text === 'string')
    .map((block) => block.text)
    .join('\n')
}

lines.on('line', (line) => {
  if (!line.trim()) return
  let message
  try {
    message = JSON.parse(line)
  } catch {
    return
  }
  switch (message.method) {
    case 'initialize':
      respond(message.id, { serverInfo: { name: 'fake-sdk-host', version: '0.0.1' } })
      break
    case 'session/prompt': {
      const sessionId = message.params?.sessionId ?? 'unknown'
      const text = promptText(message.params) || '(empty prompt)'
      respond(message.id, { messageId: `msg_${seq + 1}` })
      turn = new Promise((resolve) => {
        setTimeout(() => {
          const now = Date.now()
          const event = (type, data) => notify('session.event', {
            sessionId, event: { type, seq: ++seq, time: now, data },
          })
          event('turn/start', { turn: 1 })
          event('user/message', {
            id: 'u1', role: 'user', source: { kind: 'user' },
            content: [{ type: 'text', text }],
          })
          event('tool/call', {
            callId: 'fake-1', name: 'read_file', arguments: '{"path":"README.md"}', turn: 1, step: 1,
          })
          event('tool/result', {
            turn: 1, step: 1,
            message: {
              role: 'tool', toolCallId: 'fake-1', isError: false,
              content: [{ type: 'text', text: '# dsh-mygo' }],
            },
          })
          event('assistant/message', {
            turn: 1, step: 1,
            message: {
              id: 'a1', role: 'assistant',
              source: { kind: 'model', provider: 'deepseek-official', model: 'deepseek-flash' },
              content: [{ type: 'text', text: `Echo: ${text}` }],
            },
            stream: [],
          })
          event('turn/end', { turn: 1, reason: { kind: 'completed' } })
          notify('session.status', { sessionId, status: 'idle' })
          resolve()
        }, 20)
      })
      break
    }
    case 'session/wait':
      turn.then(() => respond(message.id, {}))
      break
    case 'shutdown':
      respond(message.id, {})
      setTimeout(() => process.exit(0), 10)
      break
    default:
      fail(message.id, `unknown method ${String(message.method)}`)
  }
})

lines.on('close', () => process.exit(0))
