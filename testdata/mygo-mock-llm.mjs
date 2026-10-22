import { LlmAdapter, ToolCallId } from '@deepseek-ai/dsh-llm'
import { defineTool } from '@deepseek-ai/dsh-tools'
import { writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { createConnection } from 'node:net'
import { once } from 'node:events'

class MygoMockAdapter extends LlmAdapter {
  async resolveModel(provider, model) {
    return { provider, id: model, name: model }
  }

  async *stream(options) {
    let reply = 'pong'
    if (options.model === 'approval-probe') {
      if (!this.probeStarted) {
        this.probeStarted = true
        const argumentsText = '{"file_path":"approval-marker.txt"}'
        const callId = ToolCallId('approval-probe-call')
        yield { type: 'block-start', index: 0, blockType: 'tool-call' }
        yield { type: 'tool-call-delta', index: 0, id: callId, name: 'mygo_approval_probe', argumentsDelta: argumentsText }
        yield { type: 'block-end', index: 0, block: { type: 'tool-call', id: callId, name: 'mygo_approval_probe', arguments: argumentsText } }
        yield { type: 'finish', reason: { kind: 'tool-calls' } }
        return
      }
      const last = options.messages.at(-1)
      if (last?.role === 'tool') reply = `Approval result: ${last.content.filter((block) => block.type === 'text').map((block) => block.text).join('')}`
    }
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
    yield { type: 'text-delta', index: 0, text: reply }
    yield { type: 'block-end', index: 0, block: { type: 'text', text: reply } }
    yield { type: 'usage', usage: { inputTokens: 1, outputTokens: 1 } }
    yield { type: 'finish', reason: { kind: 'stop' } }
  }
}

export const name = 'mygo-mock-llm'
export const inject = ['llm', 'tools', 'approval', 'workingDirectory']

export function apply(ctx) {
  ctx.llm.registerAdapter(['mygo-mock'], new MygoMockAdapter())
  ctx.tools.register(defineTool({
    name: 'mygo_approval_probe', description: 'Write a harmless marker only after explicit approval.',
    parameters: { file_path: { type: 'string', required: true } },
    output: { schema: { type: 'string' }, render: (_args, value) => [{ type: 'text', text: value }] },
    async execute(args, execution) {
      if (!execution.agent || args.file_path !== 'approval-marker.txt') throw new Error('Invalid approval fixture operation')
      const outcome = await ctx.approval.request({ agent: execution.agent, toolName: 'mygo_approval_probe', callId: execution.callId, reason: 'Write approval-marker.txt in the temporary workspace', signal: execution.signal })
      if (outcome !== 'allowed-once') return `not-executed:${outcome}`
      execution.signal.throwIfAborted()
      const cwd = ctx.workingDirectory.get(execution.agent.session)
      await writeFile(join(cwd, 'approval-marker.txt'), 'approved', { flag: 'wx', signal: execution.signal })
      return 'executed'
    },
  }))
}
