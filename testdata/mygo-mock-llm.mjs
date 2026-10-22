import { LlmAdapter } from '@deepseek-ai/dsh-llm'
import { createConnection } from 'node:net'
import { once } from 'node:events'

class MygoMockAdapter extends LlmAdapter {
  async resolveModel(provider, model) {
    return { provider, id: model, name: model }
  }

  async *stream(options) {
    if (options.model === 'wait-for-cancel' && !this.waited) {
      this.waited = true
      const port = Number(process.env.DSH_MYGO_MOCK_CONTROL.split(':').at(-1))
      const control = createConnection({ host: '127.0.0.1', port })
      try {
        await once(control, 'connect')
        control.write('started\n')
        await new Promise((resolve) => {
          if (options.signal.aborted) resolve()
          else options.signal.addEventListener('abort', resolve, { once: true })
        })
        control.end('aborted\n')
        options.signal.throwIfAborted()
      } finally {
        control.end()
      }
    }
    yield { type: 'block-start', index: 0, blockType: 'text' }
    yield { type: 'text-delta', index: 0, text: 'pong' }
    yield { type: 'block-end', index: 0, block: { type: 'text', text: 'pong' } }
    yield { type: 'usage', usage: { inputTokens: 1, outputTokens: 1 } }
    yield { type: 'finish', reason: { kind: 'stop' } }
  }
}

export const name = 'mygo-mock-llm'
export const inject = ['llm']

export function apply(ctx) {
  ctx.llm.registerAdapter(['mygo-mock'], new MygoMockAdapter())
}
