// A pure, versioned read model. DSH checkpoints it beside the log; it never
// writes titles or manufactures session events. Missing rows fall back to headers.
export function catalogDefinition(z) {
  const schema = z.object({ title: z.string().max(1024), updatedAt: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER) })
  return {
    key: 'mygoCatalog', stateVersion: 1, stateSchema: schema,
    init: header => ({ title: '', updatedAt: header.createdAt }),
    apply(state, event) {
      const title = event.type === 'session/title' ? event.data.title : state.title
      const updatedAt = Math.max(state.updatedAt, event.time)
      return title === state.title && updatedAt === state.updatedAt ? state : { title, updatedAt }
    },
    wire: { viewSchema: schema, view: state => state },
  }
}
