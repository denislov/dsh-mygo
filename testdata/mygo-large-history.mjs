// Real public persistence fixture: no direct access to JSONL or private indexes.
export const name = 'mygo-large-history'
export const inject = ['agents', 'sessionPersistence']
export async function apply(ctx) {
  if (await ctx.sessionPersistence.stat('large-history')) return
  const owner = await ctx.agents.create({ sessionId: 'history-header-probe', meta: { cwd: process.cwd() }, agentOptions: { provider: 'mygo-mock', model: 'echo' } })
  const header = { ...owner.agent.session.header, id: 'large-history' }
  await owner.dispose()
  const handle = await ctx.sessionPersistence.create(header)
  try {
    const time = Date.now()
    await handle.append(Array.from({ length: 35001 }, (_, seq) => ({ seq, time, type: 'session/title', data: { title: `Large history ${seq} ${'x'.repeat(900)}`, messageSeqs: [], source: { kind: 'user' } } })))
    await handle.flush()
    const snapshot = await ctx.sessionPersistence.stat('large-history')
    if (!(snapshot.sizeBytes > 32 * 1024 * 1024)) throw new Error('The large history fixture must exceed 32 MiB')
  } finally { await handle.close() }
}
