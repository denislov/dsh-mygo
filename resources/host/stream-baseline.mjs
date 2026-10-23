// Fold only public Agent frames. Temporary state is never written to history.
export class AssistantStreamBaseline {
  revision = 0
  active = undefined

  accept(frame) {
    if (frame.revision <= this.revision) return
    const dense = frame.revision === this.revision + 1
    this.revision = frame.revision
    if (frame.type === 'start') {
      this.active = { attemptId: frame.attemptId, turn: frame.turn, step: frame.step, nextIndex: 0, blocks: new Map(), runes: 0, truncated: false }
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
      active.blocks.set(chunk.index, { index: chunk.index, type: chunk.blockType, ...(chunk.blockType === 'text' ? { text: '' } : {}), ended: false })
    } else if (chunk.type === 'text-delta') {
      if (!block || block.type !== 'text' || block.ended) { this.active = undefined; return }
      this.append(active, block, chunk.text)
    } else if (chunk.type === 'block-end') {
      if (!block || block.ended || block.type !== chunk.block.type) { this.active = undefined; return }
      if (block.type === 'text') {
        active.runes -= [...block.text].length
        block.text = ''
        this.append(active, block, chunk.block.text)
      }
      block.ended = true
    }
  }

  append(active, block, text) {
    // Iterate instead of splitting unbounded adapter text into an extra array.
    for (const rune of text) {
      if (active.runes >= 8000) { active.truncated = true; break }
      block.text += rune
      active.runes++
    }
  }

  commit(event) {
    if (event.type === 'turn/end' || (event.type === 'assistant/message' || event.type === 'assistant/attempt') && this.active?.turn === event.data.turn && this.active?.step === event.data.step) this.active = undefined
  }

  snapshot() {
    const active = this.active
    return { revision: this.revision, ...(active ? { activeAttempt: {
      attemptId: active.attemptId, turn: active.turn, step: active.step, nextIndex: active.nextIndex,
      blocks: [...active.blocks.values()].map((block) => ({ ...block })), truncated: active.truncated,
    } } : {}) }
  }
}
