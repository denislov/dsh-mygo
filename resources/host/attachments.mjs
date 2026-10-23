/**
 * Prompt image admission for the mygo channel.
 *
 * The UI sends raster images as canonical base64 content blocks; this module
 * validates them against the deployment policy advertised at initialize and
 * hands the ordered parts to the DSH attachment seam, which stores them before
 * any user event is appended. Wire payloads never reach logs or error text.
 */

/** Raster formats the version-one attachment path accepts. */
export const imageMediaTypes = ['image/png', 'image/jpeg', 'image/webp', 'image/gif']

/**
 * Check canonical RFC 4648 base64 without a regex: a pattern match on multi-
 * megabyte payloads overflows the engine stack. Padding is only accepted at
 * the end, as canonical encodings require.
 * @param value - candidate payload string.
 * @returns whether the string is canonical base64.
 */
function isCanonicalBase64(value) {
  if (typeof value !== 'string' || value.length === 0 || value.length % 4 !== 0) return false
  let padding = 0
  if (value.endsWith('==')) padding = 2
  else if (value.endsWith('=')) padding = 1
  const end = value.length - padding
  for (let index = 0; index < end; index++) {
    const code = value.charCodeAt(index)
    const character = (code >= 65 && code <= 90) || (code >= 97 && code <= 122) || (code >= 48 && code <= 57) || code === 43 || code === 47
    if (!character) return false
  }
  for (let index = end; index < value.length; index++) if (value.charCodeAt(index) !== 61) return false
  return true
}

/** One prompt may carry 8 MiB of base64, which decodes to at most 6 MiB. */
export const maxBase64Bytes = 8 * 1024 * 1024
export const maxImageBytes = 6 * 1024 * 1024
export const maxImagesPerMessage = 8
const maxImagePixels = 64000000
const maxImageDimension = 8192
const maxTextBytes = 1024 * 1024

function fault(kind, message, code = -32000) {
  return Object.assign(new Error(message), { kind, code })
}

function invalid(message) {
  return fault('protocol/invalid-params', message, -32602)
}

function positive(value) {
  return Number.isSafeInteger(value) && value > 0 ? value : undefined
}

/**
 * Read the deployment image policy, tightened to the channel's own budget.
 * @param attachments - optional DSH attachment store.
 * @returns the advertised limits, or undefined when image prompts are unavailable.
 */
export function imagePolicy(attachments) {
  if (typeof attachments?.admitPromptContent !== 'function') return undefined
  const limits = attachments.imageLimits
  if (limits === null || typeof limits !== 'object' || !Array.isArray(limits.mediaTypes)) return undefined
  const mediaTypes = imageMediaTypes.filter((type) => limits.mediaTypes.includes(type))
  if (mediaTypes.length === 0) return undefined
  const bounded = (candidate, fallback) => Math.min(positive(candidate) ?? fallback, fallback)
  return {
    mediaTypes,
    maxBase64Bytes,
    maxImageBytes: bounded(limits.maxImageBytes, maxImageBytes),
    maxImagesPerMessage: bounded(limits.maxImagesPerMessage, maxImagesPerMessage),
    maxMessageImageBytes: bounded(limits.maxMessageImageBytes, maxImageBytes),
    maxImagePixels: bounded(limits.maxImagePixels, maxImagePixels),
    maxImageDimension: bounded(limits.maxImageDimension, maxImageDimension),
  }
}

function imageName(value) {
  if (value === undefined) return undefined
  if (typeof value !== 'string' || value.trim().length === 0 || value.length > 128 || /[\\/\u0000-\u001f\u007f]/.test(value)) {
    throw invalid('Invalid image name')
  }
  return value
}

function imagePart(block, policy, state) {
  if (typeof block.mimeType !== 'string' || !policy.mediaTypes.includes(block.mimeType)) throw invalid('Unsupported image type')
  if (typeof block.data !== 'string' || block.data.length === 0 || block.data.length > policy.maxBase64Bytes || !isCanonicalBase64(block.data)) {
    throw invalid('Image data must be canonical base64 within the prompt budget')
  }
  const decoded = Buffer.from(block.data, 'base64')
  if (decoded.length === 0 || decoded.toString('base64') !== block.data) throw invalid('Image data must be canonical base64')
  if (decoded.length > policy.maxImageBytes) throw fault('session/attachment-invalid', 'Image exceeds the supported size')
  state.bytes += decoded.length
  if (state.bytes > policy.maxMessageImageBytes) throw fault('session/attachment-invalid', 'Prompt images exceed the supported total size')
  const name = imageName(block.name)
  return { type: 'image', mediaType: block.mimeType, data: block.data, ...(name === undefined ? {} : { name }) }
}

function textPart(value) {
  if (typeof value !== 'string' || value.length > maxTextBytes || value.includes('\0')) throw invalid('Invalid content text')
  return { type: 'text', text: value }
}

/**
 * Validate prompt content blocks and split them into attachment-seam parts.
 * @param blocks - untrusted content blocks in wire order.
 * @param policy - advertised image policy, or undefined when unsupported.
 * @returns ordered parts and whether any image needs admission.
 */
export function promptContent(blocks, policy) {
  if (!Array.isArray(blocks) || blocks.length === 0 || blocks.length > 64) throw invalid('Invalid content')
  const parts = []
  const state = { bytes: 0 }
  let images = 0
  for (const block of blocks) {
    if (block === null || typeof block !== 'object' || Array.isArray(block)) throw invalid('Invalid content')
    if (block.type === 'text') {
      parts.push(textPart(block.text))
      continue
    }
    if (block.type !== 'image') throw invalid('Unsupported content block')
    if (policy === undefined) throw fault('session/attachment-invalid', 'Image prompts are not supported by this host')
    images += 1
    if (images > policy.maxImagesPerMessage) throw fault('session/attachment-invalid', 'Too many images in one prompt')
    parts.push(imagePart(block, policy, state))
  }
  if (!parts.some((part) => part.type === 'image' || part.text.trim().length > 0)) throw invalid('Prompt is empty')
  return { parts, hasImage: images > 0 }
}

/**
 * Require that the session's current model declares image input. A route whose
 * modalities are unknown stays admissible, as the DSH Host prompt endpoint does.
 * @param ctx - host context carrying the model service.
 * @param route - the session's current provider/model route.
 */
export async function assertImageRoute(ctx, route) {
  const llm = ctx.get('llm')
  if (typeof llm?.resolveModelInfo !== 'function') throw fault('session/attachment-invalid', 'This host cannot verify image input support')
  let info
  try {
    info = await llm.resolveModelInfo(route.provider, route.model)
  } catch {
    throw fault('session/attachment-invalid', 'Could not verify image input support for the current model')
  }
  if (info?.inputModalities !== undefined && (!Array.isArray(info.inputModalities) || !info.inputModalities.includes('image'))) {
    throw fault('session/attachment-invalid', `Model "${route.model}" does not support image input`)
  }
}

/**
 * Persist prompt images through the attachment seam and return message content.
 * A refused image batch yields a safe admission error that carries no payload.
 * @param ctx - host context carrying the attachment store.
 * @param parts - validated parts in wire order.
 * @returns ordered content with image parts replaced by durable references.
 */
export async function admitPrompt(ctx, parts) {
  const attachments = ctx.get('attachments')
  if (typeof attachments?.admitPromptContent !== 'function') throw fault('session/attachment-invalid', 'Image prompts are not supported by this host')
  try {
    return await attachments.admitPromptContent(parts)
  } catch (error) {
    const reason = typeof error?.code === 'string' && /^[A-Z][A-Z0-9_]{2,63}$/.test(error.code)
    const message = reason && typeof error.message === 'string' && error.message.length <= 200 ? error.message : 'Could not store the prompt image'
    throw fault('session/attachment-invalid', message)
  }
}
