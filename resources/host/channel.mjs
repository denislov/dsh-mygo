import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import Schema from '@deepseek-ai/schemastery'
import { createUserMessage, ReasoningEffortId } from '@deepseek-ai/dsh-llm'
import * as DeepSeek from '@deepseek-ai/dsh-llm-deepseek-api-key'
import { LineChannel } from './transport.mjs'
import { MygoServer } from './server.mjs'

export const name = 'mygo-channel'
export const inject = ['agents', 'llm', 'mygoAppStartup', 'loader', 'appExit', 'appReady', 'profileContext']
export const Config = Schema.object({
  maxFrameBytes: Schema.natural().min(1024).max(16 * 1024 * 1024).default(16 * 1024 * 1024),
  maxQueuedBytes: Schema.natural().min(1024).max(64 * 1024 * 1024).default(32 * 1024 * 1024),
  maxSessions: Schema.natural().min(1).max(64).default(8),
  cancelTimeoutMs: Schema.natural().min(1).max(60000).default(5000),
  maxPendingApprovals: Schema.natural().min(1).max(64).default(32),
  maxPendingQuestions: Schema.natural().min(1).max(64).default(32),
})

export function apply(ctx, config) {
  const profile = ctx.get('profileContext')
  if (!profile?.startedBundles.includes('@deepseek-ai/dsh-base') || !profile.startedBundles.includes('@dsh-mygo/host')) {
    throw new Error('The mygo host requires the dsh-base and @dsh-mygo/host profile bundles')
  }
  const channel = new LineChannel(process.stdin, process.stdout, config)
  const dshVersion = JSON.parse(readFileSync(fileURLToPath(import.meta.resolve('@deepseek-ai/dsh-llm/package.json')), 'utf8')).version
  const server = new MygoServer(ctx, {
    notify: (method, params) => channel.notify(method, params),
    createUserMessage, reasoningEffort: ReasoningEffortId, dshVersion,
    maxSessions: config.maxSessions,
    cancelTimeoutMs: config.cancelTimeoutMs,
    maxPendingApprovals: config.maxPendingApprovals,
    maxPendingQuestions: config.maxPendingQuestions,
    ensureProvider: async (provider) => {
      if (ctx.llm.listProviders().some((entry) => entry.id === provider)) return
      if (provider !== 'deepseek-official') throw new Error('Model provider is unavailable')
      await ctx.plugin(DeepSeek)
    },
  })
  ctx.on('approval/request', (request, next) => server.requestApproval(request, next))
  ctx.on('user-questions/request', (request, next) => server.requestQuestion(request, next))
  ctx.effect(() => ctx.appReady.onReady(() => {
    channel.start(async (method, params) => {
      if (method === 'initialize') await ctx.loader.await()
      if (method === 'shutdown') {
        server.stopping = true
        setImmediate(async () => { await channel.flush(); ctx.appExit(0) })
        return {}
      }
      try {
        const result = await server.handle(method, params)
        return result
      } catch (error) {
        if (['protocol/version-mismatch', 'protocol/runtime-mismatch', 'protocol/release-mismatch'].includes(error.kind)) {
          setImmediate(async () => { await channel.flush(); ctx.appExit(1) })
        }
        throw error
      }
    }, (error) => ctx.appExit(error ? 1 : 0))
  }), 'mygo.channel-ready')
  ctx.effect(() => async () => {
    channel.close()
    await server.shutdown()
  }, 'mygo.channel-dispose')
}
