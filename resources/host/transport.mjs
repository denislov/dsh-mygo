import { TextDecoder } from 'node:util'

export class LineChannel {
  constructor(input, output, { maxFrameBytes, maxQueuedBytes = maxFrameBytes * 2 }) {
    this.input = input
    this.output = output
    this.maxFrameBytes = maxFrameBytes
    this.maxQueuedBytes = maxQueuedBytes
    this.queuedBytes = 0
    this.writes = Promise.resolve()
    this.closed = false
    this.output.on('error', () => this.close(new Error('Host protocol output failed')))
  }

  start(handler, onClose) {
    this.onClose = onClose
    void this.read(handler)
  }

  async read(handler) {
    let pending = Buffer.alloc(0)
    try {
      for await (const chunk of this.input) {
        let remaining = Buffer.from(chunk)
        while (remaining.length > 0 && !this.closed) {
          const newline = remaining.indexOf(10)
          const length = newline < 0 ? remaining.length : newline + 1
          if (pending.length + length > this.maxFrameBytes) throw new Error('Protocol frame too large')
          pending = Buffer.concat([pending, remaining.subarray(0, length)])
          remaining = remaining.subarray(length)
          if (newline < 0) break
          const frame = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(pending))
          pending = Buffer.alloc(0)
          this.validate(frame)
          try {
            const result = await handler(frame.method, frame.params ?? {})
            await this.send({ jsonrpc: '2.0', id: frame.id, result })
          } catch (error) {
            await this.send({ jsonrpc: '2.0', id: frame.id, error: {
              code: Number.isInteger(error.code) ? error.code : -32603,
              message: error.kind ? error.message : 'Host request failed',
              data: { kind: error.kind ?? 'host/internal' },
            } })
          }
        }
        if (this.closed) return
      }
      if (pending.length > 0) throw new Error('Incomplete protocol frame')
      await this.flush()
      this.close()
    } catch {
      this.close(new Error('Host protocol connection failed'))
    }
  }

  validate(frame) {
    if (frame === null || typeof frame !== 'object' || Array.isArray(frame)
      || frame.jsonrpc !== '2.0' || typeof frame.method !== 'string' || frame.method.length === 0
      || frame.method.length > 128 || !Object.hasOwn(frame, 'id')
      || !(typeof frame.id === 'string' && frame.id.length > 0 && frame.id.length <= 128
        || Number.isSafeInteger(frame.id))
      || Object.hasOwn(frame, 'result') || Object.hasOwn(frame, 'error')
      || frame.params !== undefined && (frame.params === null || typeof frame.params !== 'object' || Array.isArray(frame.params))) {
      throw new Error('Invalid protocol frame')
    }
  }

  send(message) {
    if (this.closed) return Promise.reject(new Error('Channel closed'))
    const wire = Buffer.from(`${JSON.stringify(message)}\n`)
    if (wire.length > this.maxFrameBytes || this.queuedBytes + wire.length > this.maxQueuedBytes) {
      this.close(new Error('Host protocol output limit exceeded'))
      return Promise.reject(new Error('Host protocol output limit exceeded'))
    }
    this.queuedBytes += wire.length
    const write = this.writes.then(() => new Promise((resolve, reject) => {
      this.output.write(wire, (error) => error ? reject(error) : resolve())
    })).finally(() => { this.queuedBytes -= wire.length })
    this.writes = write.catch(() => this.close(new Error('Host protocol output failed')))
    return write
  }

  notify(method, params) {
    void this.send({ jsonrpc: '2.0', method, params }).catch(() => {})
  }

  async flush() { await this.writes }

  close(error) {
    if (this.closed) return
    this.closed = true
    this.input.destroy()
    this.onClose?.(error)
  }
}
