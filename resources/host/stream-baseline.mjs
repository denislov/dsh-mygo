// Fold only public Agent frames. Temporary state is never written to history.
export class AssistantStreamBaseline {
  revision = 0
  active = undefined

  accept(frame) {
    if (frame.revision <= this.revision) return
    const dense = frame.revision === this.revision + 1
    this.revision = frame.revision
    if (frame.type === 'start') {
      this.active = { attemptId: frame.attemptId, turn: frame.turn, step: frame.step, nextIndex: 0, blocks: new Map(), runes: 0, reasoningRunes: 0, truncated: false, reasoningTruncated: false }
      return
    }
    const active = this.active
    if (!dense || !active || active.attemptId !== frame.attemptId || active.nextIndex !== frame.index || frame.type === 'end') {
      this.active = undefined
      return
    }
    active.nextIndex++
    const chunk = frame.chunk
    const block = active.blocks.get(chunk.index)
    if (chunk.type === 'block-start') {
      if (block || active.blocks.size >= 128) { this.active = undefined; return }
      const textual = chunk.blockType === 'text' || chunk.blockType === 'reasoning'
      active.blocks.set(chunk.index, { index: chunk.index, type: chunk.blockType, ...(textual ? { text: '' } : {}), ended: false })
    } else if (chunk.type === 'text-delta' || chunk.type === 'reasoning-delta') {
      const type = chunk.type === 'text-delta' ? 'text' : 'reasoning'
      if (!block || block.type !== type || block.ended) { this.active = undefined; return }
      this.append(active, block, chunk.text)
    } else if (chunk.type === 'block-end') {
      if (!block || block.ended || block.type !== chunk.block.type) { this.active = undefined; return }
      if (block.type === 'text' || block.type === 'reasoning') {
        this.resize(active, block, -[...block.text].length)
        block.text = ''
        this.append(active, block, chunk.block.text)
      }
      block.ended = true
    }
  }

  resize(active, block, delta) {
    if (block.type === 'reasoning') active.reasoningRunes += delta
    else active.runes += delta
  }

  append(active, block, text) {
    const reasoning = block.type === 'reasoning'
    const limit = 8000
    // Iterate instead of splitting unbounded adapter text into an extra array.
    for (const rune of text) {
      const used = reasoning ? active.reasoningRunes : active.runes
      if (used >= limit) {
        if (reasoning) active.reasoningTruncated = true
        else active.truncated = true
        break
      }
      block.text += rune
      if (reasoning) active.reasoningRunes++
      else active.runes++
    }
  }

  commit(event) {
    if (event.type === 'turn/end' || (event.type === 'assistant/message' || event.type === 'assistant/attempt') && this.active?.turn === event.data.turn && this.active?.step === event.data.step) this.active = undefined
  }

  snapshot(includeReasoning = false) {
    const active = this.active
    if (!active) return { revision: this.revision }
    const blocks = [...active.blocks.values()].map((block) => {
      if (block.type !== 'reasoning' || includeReasoning) return { ...block }
      const { text, ...rest } = block
      return rest
    })
    return { revision: this.revision, activeAttempt: {
      attemptId: active.attemptId, turn: active.turn, step: active.step, nextIndex: active.nextIndex,
      blocks, truncated: active.truncated || (includeReasoning && active.reasoningTruncated),
    } }
  }
}
