import { LlmAdapter, ToolCallId } from '@deepseek-ai/dsh-llm'
import { defineTool } from '@deepseek-ai/dsh-tools'
import { writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { createConnection } from 'node:net'
import { once } from 'node:events'
import { markdownPrefix, markdownReply } from './markdown-sample.mjs'

class MygoMockAdapter extends LlmAdapter {
  async listModels(provider) {
    return [{ provider, id: 'echo', name: 'Echo' }, { provider, id: 'route-probe', name: 'Route probe' }, { provider, id: 'todo-probe', name: 'Todo probe' }]
  }

  async resolveModel(provider, model) {
    return { provider, id: model, name: model, ...(model === 'route-probe' ? { reasoning: { efforts: [{ id: 'low', name: 'Low' }, { id: 'high', name: 'High' }] } } : {}) }
  }

  async *stream(options) {
    let reply = 'pong'
    if (options.model === 'route-probe') reply = `Selected ${options.provider}/${options.model}/${options.reasoningEffort ?? 'default'}`
    if (options.model === 'todo-probe') {
      const step = this.todoCalls ?? 0
      if (step < 1) {
        this.todoCalls = 1
        const argumentsText = JSON.stringify({ todos: [{ content: 'Inspect projection', status: 'completed' }, { content: 'Project todo panel', status: 'in_progress' }] })
        const callId = ToolCallId('todo-probe-0')
        yield { type: 'block-start', index: 0, blockType: 'tool-call' }
        yield { type: 'tool-call-delta', index: 0, id: callId, name: 'todo_write', argumentsDelta: argumentsText }
        yield { type: 'block-end', index: 0, block: { type: 'tool-call', id: callId, name: 'todo_write', arguments: argumentsText } }
        yield { type: 'finish', reason: { kind: 'tool-calls' } }
        return
      }
      if (options.messages.at(-1)?.role === 'tool') reply = 'todo probe complete'
    }
    if (options.model === 'diff-probe') {
      const step = this.diffCalls ?? 0
      if (step < 2) {
        this.diffCalls = step + 1
        const toolName = step === 0 ? 'write' : 'edit'
        const argumentsText = JSON.stringify(step === 0
          ? { file_path: 'diff-probe.txt', content: 'before\n' }
          : { file_path: 'diff-probe.txt', old_string: 'before', new_string: 'after' })
        const callId = ToolCallId(`diff-probe-${step}`)
        yield { type: 'block-start', index: 0, blockType: 'tool-call' }
        yield { type: 'tool-call-delta', index: 0, id: callId, name: toolName, argumentsDelta: argumentsText }
        yield { type: 'block-end', index: 0, block: { type: 'tool-call', id: callId, name: toolName, arguments: argumentsText } }
        yield { type: 'finish', reason: { kind: 'tool-calls' } }
        return
      }
      if (options.messages.at(-1)?.role === 'tool') reply = 'diff probe complete'
    }
    if ((options.model === 'stream-probe' || options.model === 'markdown-probe') && !this.streamed) {
      this.streamed = true
      yield { type: 'block-start', index: 0, blockType: 'reasoning' }
      yield { type: 'reasoning-delta', index: 0, text: 'Hidden fixture reasoning' }
      yield { type: 'block-end', index: 0, block: { type: 'reasoning', text: 'Hidden fixture reasoning' } }
      yield { type: 'block-start', index: 1, blockType: 'text' }
      yield { type: 'text-delta', index: 1, text: options.model === 'markdown-probe' ? markdownPrefix : 'Live prefix' }
      const port = Number(process.env.DSH_MYGO_MOCK_CONTROL.split(':').at(-1))
      const control = createConnection({ host: '127.0.0.1', port })
      try {
        await once(control, 'connect')
        control.write('paused\n')
        await new Promise((resolve, reject) => {
          const onAbort = () => { cleanup(); reject(options.signal.reason) }
          const onData = () => { cleanup(); resolve() }
          const onError = () => { cleanup(); reject(new Error('Fixture control connection failed')) }
          const cleanup = () => {
            options.signal.removeEventListener('abort', onAbort)
            control.off('data', onData)
            control.off('error', onError)
            control.off('end', onError)
          }
          options.signal.addEventListener('abort', onAbort, { once: true })
          control.once('data', onData)
          control.once('error', onError)
          control.once('end', onError)
          if (options.signal.aborted) onAbort()
        })
      } finally { control.destroy() }
      options.signal.throwIfAborted()
      yield { type: 'text-delta', index: 1, text: options.model === 'markdown-probe' ? markdownReply.slice(markdownPrefix.length) : ' complete' }
      yield { type: 'block-end', index: 1, block: { type: 'text', text: options.model === 'markdown-probe' ? markdownReply : 'Live prefix complete' } }
      yield { type: 'finish', reason: { kind: 'stop' } }
      return
    }
    if (options.model === 'approval-probe' || options.model === 'question-probe') {
      if (!this.probeStarted) {
        this.probeStarted = true
        const isQuestion = options.model === 'question-probe'
        const toolName = isQuestion ? 'ask_user_question' : 'mygo_approval_probe'
        const argumentsText = isQuestion ? JSON.stringify({ questions: [
          { id: 'mode', question: 'Choose mode', options: [{ label: 'Safe', description: 'Recommended mode' }, { label: 'Fast' }] },
          { id: 'features', question: 'Choose features', multi_select: true, options: [{ label: 'Logs' }, { label: 'Tests' }] },
          { id: 'note', question: 'Add a note' },
        ] }) : '{"file_path":"approval-marker.txt"}'
        const callId = ToolCallId(isQuestion ? 'question-probe-call' : 'approval-probe-call')
        yield { type: 'block-start', index: 0, blockType: 'tool-call' }
        yield { type: 'tool-call-delta', index: 0, id: callId, name: toolName, argumentsDelta: argumentsText }
        yield { type: 'block-end', index: 0, block: { type: 'tool-call', id: callId, name: toolName, arguments: argumentsText } }
        yield { type: 'finish', reason: { kind: 'tool-calls' } }
        return
      }
      const last = options.messages.at(-1)
      if (last?.role === 'tool') reply = `${options.model === 'question-probe' ? 'Question' : 'Approval'} result: ${last.content.filter((block) => block.type === 'text').map((block) => block.text).join('')}`
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
