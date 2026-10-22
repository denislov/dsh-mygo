import { randomUUID } from 'node:crypto'

function questionError(code) {
  return Object.assign(new Error('The user question is unavailable or was cancelled'), { name: 'UserQuestionError', code })
}
function invalid() {
  return Object.assign(new Error('Invalid question response'), { kind: 'protocol/invalid-params', code: -32602 })
}
function record(value) { return value !== null && typeof value === 'object' && !Array.isArray(value) }
function text(value, limit, required = false) {
  return typeof value === 'string' && (!required || value.trim().length > 0) && Buffer.byteLength(value) <= limit && !value.includes('\0')
}
function normalizeQuestions(questions) {
  if (!Array.isArray(questions) || questions.length < 1 || questions.length > 8) throw questionError('BAD_REQUEST')
  const ids = new Set()
  const normalized = questions.map((question) => {
    if (!record(question) || !text(question.id, 128, true) || ids.has(question.id) || !text(question.question, 4096, true)
      || question.header !== undefined && !text(question.header, 256)
      || question.detail !== undefined && !text(question.detail, 32768)
      || question.multiSelect !== undefined && typeof question.multiSelect !== 'boolean') throw questionError('BAD_REQUEST')
    ids.add(question.id)
    const labels = new Set()
    if (question.options !== undefined && (!Array.isArray(question.options) || question.options.length > 16)) throw questionError('BAD_REQUEST')
    const options = question.options?.map((option) => {
      if (!record(option) || !text(option.label, 256, true) || labels.has(option.label) || option.description !== undefined && !text(option.description, 2048)) throw questionError('BAD_REQUEST')
      labels.add(option.label)
      return { label: option.label, ...(option.description === undefined ? {} : { description: option.description }) }
    })
    if (question.intent !== undefined && (!record(question.intent) || question.intent.kind !== 'plan-review' || !labels.has(question.intent.approve) || !text(question.detail, 32768, true))) throw questionError('BAD_REQUEST')
    return { id: question.id, question: question.question,
      ...(question.header === undefined ? {} : { header: question.header }),
      ...(question.detail === undefined ? {} : { detail: question.detail }),
      ...(question.multiSelect === undefined ? {} : { multiSelect: question.multiSelect }),
      ...(options === undefined ? {} : { options }),
    }
  })
  if (Buffer.byteLength(JSON.stringify(normalized)) > 65536) throw questionError('BAD_REQUEST')
  return normalized
}

export class QuestionQueue {
  constructor({ notify, maxPending = 32 }) {
    this.notify = notify
    this.maxPending = maxPending
    this.pending = new Map()
    this.closed = false
  }

  async ask(request, sessionId) {
    if (this.closed) throw questionError('ASK_CANCELLED')
    if (request.signal?.aborted) throw questionError('ASK_ABORTED')
    if (request.wait?.timed || this.pending.size >= this.maxPending) throw questionError('NO_PROVIDER')
    const questions = normalizeQuestions(request.questions)
    const requestId = randomUUID()
    return new Promise((resolve, reject) => {
      const entry = { requestId, sessionId, questions, resolve, reject, signal: request.signal,
        onAbort: () => this.settle(requestId, undefined, 'ASK_ABORTED') }
      this.pending.set(requestId, entry)
      entry.signal?.addEventListener('abort', entry.onAbort, { once: true })
      if (entry.signal?.aborted) { entry.onAbort(); return }
      this.notify('question/request', { requestId, sessionId, questions })
    })
  }

  entry(params) {
    if (!record(params) || !text(params.requestId, 128, true) || !/^[a-zA-Z0-9_-]+$/.test(params.requestId)) throw invalid()
    const entry = this.pending.get(params.requestId)
    if (!entry) throw Object.assign(new Error('The question is no longer pending'), { kind: 'question/stale-request', code: -32000 })
    return entry
  }

  respond(params) {
    const entry = this.entry(params)
    if (!Array.isArray(params.answers) || params.answers.length !== entry.questions.length || Buffer.byteLength(JSON.stringify(params.answers)) > 65536) throw invalid()
    const ids = new Set()
    const answers = params.answers.map((answer) => {
      if (!record(answer) || ids.has(answer.id)) throw invalid()
      const question = entry.questions.find((item) => item.id === answer.id)
      if (!question || !Array.isArray(answer.selected) || answer.selected.length > 16
        || answer.custom !== undefined && !text(answer.custom, 8192)) throw invalid()
      ids.add(answer.id)
      const labels = new Set((question.options ?? []).map((option) => option.label))
      if (answer.selected.some((label) => !labels.has(label)) || new Set(answer.selected).size !== answer.selected.length) throw invalid()
      const hasCustom = !!answer.custom?.trim()
      if (answer.selected.length === 0 && !hasCustom || !question.multiSelect && answer.selected.length + Number(hasCustom) !== 1) throw invalid()
      return { id: answer.id, selected: [...answer.selected], ...(answer.custom === undefined ? {} : { custom: answer.custom }) }
    })
    this.settle(entry.requestId, { answers })
    return {}
  }

  cancel(params) {
    const entry = this.entry(params)
    this.settle(entry.requestId, undefined, 'ASK_CANCELLED')
    return {}
  }

  settle(requestId, answer, code) {
    const entry = this.pending.get(requestId)
    if (!entry) return
    this.pending.delete(requestId)
    entry.signal?.removeEventListener('abort', entry.onAbort)
    this.notify('question/closed', { requestId, sessionId: entry.sessionId })
    if (code) entry.reject(questionError(code))
    else entry.resolve(answer)
  }

  cancelSession(sessionId) {
    for (const entry of this.pending.values()) if (entry.sessionId === sessionId) this.settle(entry.requestId, undefined, 'ASK_CANCELLED')
  }

  close() {
    this.closed = true
    for (const entry of this.pending.values()) this.settle(entry.requestId, undefined, 'ASK_CANCELLED')
  }
}
