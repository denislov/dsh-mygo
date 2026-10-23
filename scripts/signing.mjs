import { spawnSync } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import {
  chmodSync,
  closeSync,
  existsSync,
  mkdirSync,
  openSync,
  readFileSync,
  readSync,
  readdirSync,
  renameSync,
  rmSync,
  writeFileSync,
} from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

export const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
export const signingDir = join(root, '.signing')
export const pfxPath = join(signingDir, 'dsh-mygo.pfx')
export const certPath = join(signingDir, 'dsh-mygo.cer')
export const pemPath = join(signingDir, 'dsh-mygo.pem')
export const passwordPath = join(signingDir, 'password.txt')
export const subjectPath = join(signingDir, 'subject.txt')
export const defaultSubject = '/CN=dsh-mygo (self-signed)/O=dsh-mygo'
export const defaultTimestamp = 'http://timestamp.digicert.com'

// The signing identity is generated here, never committed. The PFX holds the
// private key; password.txt and subject.txt are local build state.
export function ensureCertificate({ force = false, quiet = false } = {}) {
  const subject = normalizeSubject(process.env.DSH_MYGO_SIGN_SUBJECT ?? defaultSubject)
  if (!force && existsSync(pfxPath) && existsSync(passwordPath)) {
    const password = readFileSync(passwordPath, 'utf8').trim()
    if (!password) throw new Error(`${passwordPath} is empty; run node scripts/self-sign.mjs --force`)
    return { created: false, subject: readSubjectCN(), pfx: pfxPath, cert: certPath, password }
  }
  if (!force && existsSync(pfxPath)) {
    throw new Error(`${pfxPath} exists but ${passwordPath} is missing; run node scripts/self-sign.mjs --force`)
  }

  const openssl = requireOpenSSL()
  const days = readInteger('DSH_MYGO_SIGN_DAYS', 1095, 1, 3650)
  const bits = readInteger('DSH_MYGO_SIGN_KEY_BITS', 3072, 2048, 4096)
  if (force && existsSync(pfxPath) && !quiet) {
    console.warn('Replacing the existing self-signed certificate; earlier signatures will no longer verify against it.')
  }

  mkdirSync(signingDir, { recursive: true, mode: 0o700 })
  const temp = join(signingDir, `.tmp-${process.pid}-${Date.now()}`)
  mkdirSync(temp, { recursive: true, mode: 0o700 })
  const keyPath = join(temp, 'key.pem')
  const pemTemp = join(temp, 'cert.pem')
  const pfxTemp = join(temp, 'dsh-mygo.pfx')
  const cerTemp = join(temp, 'dsh-mygo.cer')
  const password = randomBytes(24).toString('base64url')

  try {
    runOrThrow(
      openssl,
      [
        'req', '-x509', '-newkey', `rsa:${bits}`, '-sha256', '-days', String(days), '-nodes',
        '-keyout', keyPath, '-out', pemTemp, '-subj', subject,
        '-addext', 'keyUsage=critical,digitalSignature',
        '-addext', 'extendedKeyUsage=critical,codeSigning',
        '-addext', 'basicConstraints=critical,CA:FALSE',
      ],
      'openssl req',
    )
    runOrThrow(
      openssl,
      [
        'pkcs12', '-export', '-out', pfxTemp, '-inkey', keyPath, '-in', pemTemp,
        '-passout', `pass:${password}`,
        '-keypbe', 'AES-256-CBC', '-certpbe', 'AES-256-CBC', '-macalg', 'sha256',
      ],
      'openssl pkcs12',
    )
    runOrThrow(openssl, ['pkcs12', '-in', pfxTemp, '-passin', `pass:${password}`, '-noout'], 'openssl pkcs12 check')
    runOrThrow(openssl, ['x509', '-in', pemTemp, '-outform', 'DER', '-out', cerTemp], 'openssl x509')

    // Write the password first: a crash must not leave a PFX whose password is
    // missing, because that would force an unnecessary identity change.
    writeFileSync(passwordPath, `${password}\n`, { mode: 0o600 })
    try {
      chmodSync(passwordPath, 0o600)
    } catch {
      // Windows does not implement Unix modes; the file is still user-local.
    }
    rmSync(pfxPath, { force: true })
    renameSync(pfxTemp, pfxPath)
    rmSync(certPath, { force: true })
    renameSync(cerTemp, certPath)
    rmSync(pemPath, { force: true })
    renameSync(pemTemp, pemPath)
    writeFileSync(subjectPath, `${subjectCN(subject)}\n`)
  } finally {
    rmSync(temp, { recursive: true, force: true })
  }

  if (!quiet) {
    const info = certificateInfo()
    console.log('Created self-signed code signing certificate')
    console.log(`  subject: ${info.subject}`)
    console.log(`  sha1:    ${info.sha1}`)
    console.log(`  sha256:  ${info.sha256}`)
    console.log(`  pfx:     ${relative(root, pfxPath)}`)
    console.log(`  cert:    ${relative(root, certPath)}`)
  }
  return { created: true, subject: subjectCN(subject), pfx: pfxPath, cert: certPath, password }
}

export function certificateInfo() {
  const openssl = requireOpenSSL()
  if (!existsSync(pemPath)) throw new Error(`missing ${pemPath}; run node scripts/self-sign.mjs`)
  const subject = runOrThrow(
    openssl,
    ['x509', '-in', pemPath, '-noout', '-subject', '-nameopt', 'RFC2253'],
    'openssl x509',
  ).stdout.split('\n').find((line) => line.startsWith('subject='))?.slice('subject='.length).trim() ?? ''
  const dates = runOrThrow(openssl, ['x509', '-in', pemPath, '-noout', '-dates'], 'openssl x509').stdout
  return {
    subject,
    sha1: fingerprint(openssl, 'sha1'),
    sha256: fingerprint(openssl, 'sha256'),
    notBefore: dates.match(/notBefore=(.+)/)?.[1]?.trim() ?? '',
    notAfter: dates.match(/notAfter=(.+)/)?.[1]?.trim() ?? '',
  }
}

export function readSubjectCN() {
  if (existsSync(subjectPath)) {
    const value = readFileSync(subjectPath, 'utf8').trim()
    if (value) return value
  }
  return subjectCN(process.env.DSH_MYGO_SIGN_SUBJECT ?? defaultSubject)
}

export function readSigningMaterial() {
  const pfx = process.env.DSH_MYGO_SIGN_PFX
  const password = process.env.DSH_MYGO_SIGN_PASSWORD
  if (pfx || password) {
    if (!pfx || !password) throw new Error('DSH_MYGO_SIGN_PFX and DSH_MYGO_SIGN_PASSWORD must be set together')
    if (!existsSync(pfx)) throw new Error(`DSH_MYGO_SIGN_PFX does not exist: ${pfx}`)
    return { pfx, password }
  }
  if (!existsSync(pfxPath)) return null
  if (!existsSync(passwordPath)) throw new Error(`${passwordPath} is missing; run node scripts/self-sign.mjs --force`)
  const value = readFileSync(passwordPath, 'utf8').trim()
  if (!value) throw new Error(`${passwordPath} is empty; run node scripts/self-sign.mjs --force`)
  return { pfx: pfxPath, password: value }
}

export function signFile(file) {
  if (process.platform !== 'win32') throw new Error('Windows code signing is only available on Windows')
  if (!existsSync(file)) throw new Error(`file not found: ${file}`)
  if (process.env.DSH_MYGO_SIGN === '0') {
    warnOnce('disabled', 'DSH_MYGO_SIGN=0: signing is disabled; the build output is not Authenticode-signed')
    return { status: 'skipped' }
  }

  const signtool = findSigntool()
  if (!signtool) throw new Error('signtool.exe was not found; install the Windows SDK or set DSH_MYGO_SIGNTOOL')
  let material = readSigningMaterial()
  if (!material) {
    const created = ensureCertificate()
    material = { pfx: created.pfx, password: created.password }
  }

  const timestamp = process.env.DSH_MYGO_TIMESTAMP_URL ?? defaultTimestamp
  const base = ['sign', '/f', material.pfx, '/p', material.password, '/fd', 'sha256']
  const args = timestamp
    ? [...base, '/tr', timestamp, '/td', 'sha256', '/v', file]
    : [...base, '/v', file]
  let result = runTool(signtool, args)
  if (result.error) throw new Error(`signtool: ${result.error.message}`)
  if (result.status !== 0 && timestamp && /timestamp/i.test(toolOutput(result))) {
    warnOnce(
      'timestamp',
      `Timestamp server ${timestamp} failed; signing without a timestamp (acceptable for a self-signed certificate)`,
    )
    result = runTool(signtool, [...base, '/v', file])
  }
  if (result.error) throw new Error(`signtool: ${result.error.message}`)
  if (result.status !== 0) throw new Error(`signtool could not sign ${file}:\n${toolOutput(result)}`)
  return { status: 'signed' }
}

export function trustCertificate() {
  if (process.platform !== 'win32') throw new Error('Trusting the certificate is only available on Windows')
  const certutil = findCertutil()
  if (!certutil) throw new Error('certutil.exe was not found')
  if (!existsSync(certPath)) ensureCertificate({ quiet: true })
  console.log(`Trusting ${readSubjectCN()} for the current user...`)
  for (const store of ['Root', 'TrustedPublisher']) {
    const result = runTool(certutil, ['-addstore', '-user', store, certPath])
    if (result.status !== 0) {
      throw new Error(`certutil could not add the certificate to ${store}:\n${toolOutput(result)}`)
    }
  }
  console.log(`Trusted ${readSubjectCN()} for the current user (Root and TrustedPublisher).`)
  console.log('Remove it with: node scripts/self-sign.mjs --remove-trust')
}

export function removeTrust() {
  if (process.platform !== 'win32') throw new Error('Removing trust is only available on Windows')
  const certutil = findCertutil()
  if (!certutil) throw new Error('certutil.exe was not found')
  if (!existsSync(certPath)) throw new Error(`missing ${certPath}; no certificate to remove`)
  const info = certificateInfo()
  if (!info.sha1) throw new Error('could not read the certificate thumbprint')
  console.log(`Removing ${readSubjectCN()} from the current user's trust stores...`)
  for (const store of ['Root', 'TrustedPublisher']) {
    const result = runTool(certutil, ['-delstore', '-user', store, info.sha1])
    if (result.status !== 0) {
      throw new Error(`certutil could not remove the certificate from ${store}:\n${toolOutput(result)}`)
    }
  }
  console.log(`Removed ${readSubjectCN()} from the current user's trust stores.`)
}

export function verifyFile(file) {
  const signtool = findSigntool()
  if (!signtool) return { ok: false, reason: 'signtool.exe was not found' }
  const result = runTool(signtool, ['verify', '/pa', '/v', file])
  const detail = toolOutput(result)
  // The signer and the timestamp authority each have an "Issued to" line.
  const subjects = [...detail.matchAll(/Issued to:\s*(.+)/g)].map((match) => match[1].trim()).filter(Boolean)
  const subject = subjects[0] ?? ''
  if (result.error) return { ok: false, reason: result.error.message, detail, subject, subjects }
  if (result.status === 0) return { ok: true, trusted: true, subject, subjects, detail }
  if (/not trusted|0x800B0109|terminated in a root certificate/i.test(detail)) {
    return { ok: true, trusted: false, subject, subjects, detail }
  }
  return { ok: false, reason: 'signtool verify failed', detail, subject, subjects }
}

// hasAuthenticodeSignature reads the PE optional header's certificate table.
// It answers whether a signature is present, not whether it is trusted.
export function hasAuthenticodeSignature(file) {
  let fd
  try {
    fd = openSync(file, 'r')
    const dos = Buffer.alloc(64)
    if (readSync(fd, dos, 0, dos.length, 0) !== dos.length || dos.toString('ascii', 0, 2) !== 'MZ') return false
    const peOffset = dos.readUInt32LE(0x3c)
    const headers = Buffer.alloc(24 + 240)
    const read = readSync(fd, headers, 0, headers.length, peOffset)
    if (read < 24 || headers.toString('ascii', 0, 4) !== 'PE\0\0') return false
    const optionalSize = headers.readUInt16LE(20)
    const magic = headers.readUInt16LE(24)
    const dirsOffset = magic === 0x10b ? 96 : magic === 0x20b ? 112 : null
    if (dirsOffset === null || optionalSize < dirsOffset + 5 * 8) return false
    // PE signature (4) + COFF header (20) + optional header fields + the
    // security directory (index 4) follow one another in that order.
    const security = 24 + dirsOffset + 4 * 8
    if (security + 8 > read) return false
    return headers.readUInt32LE(security + 4) !== 0
  } catch {
    return false
  } finally {
    if (fd !== undefined) closeSync(fd)
  }
}

export function findOpenSSL() {
  const override = process.env.DSH_MYGO_OPENSSL
  if (override) {
    if (!existsSync(override)) throw new Error(`DSH_MYGO_OPENSSL does not exist: ${override}`)
    return override
  }
  const fromPath = findOnPath('openssl')
  if (fromPath) return fromPath
  if (process.platform !== 'win32') return null
  const programFiles = process.env.ProgramFiles ?? 'C:\\Program Files'
  const programFilesX86 = process.env['ProgramFiles(x86)'] ?? 'C:\\Program Files (x86)'
  const candidates = [
    join(programFiles, 'Git', 'usr', 'bin', 'openssl.exe'),
    join(programFiles, 'Git', 'mingw64', 'bin', 'openssl.exe'),
    join(programFilesX86, 'Git', 'usr', 'bin', 'openssl.exe'),
    join(programFiles, 'OpenSSL-Win64', 'bin', 'openssl.exe'),
    join(programFiles, 'OpenSSL', 'bin', 'openssl.exe'),
  ]
  return candidates.find((candidate) => existsSync(candidate)) ?? null
}

export function findSigntool() {
  if (process.platform !== 'win32') return null
  const override = process.env.DSH_MYGO_SIGNTOOL
  if (override) {
    if (!existsSync(override)) throw new Error(`DSH_MYGO_SIGNTOOL does not exist: ${override}`)
    return override
  }
  const fromPath = findOnPath('signtool')
  if (fromPath) return fromPath
  const arch = process.arch === 'arm64' ? 'arm64' : 'x64'
  const roots = [
    process.env['ProgramFiles(x86)'] ?? 'C:\\Program Files (x86)',
    process.env.ProgramFiles ?? 'C:\\Program Files',
  ]
  const matches = []
  for (const base of roots) {
    const bin = join(base, 'Windows Kits', '10', 'bin')
    if (!existsSync(bin)) continue
    for (const entry of readdirSync(bin, { withFileTypes: true })) {
      if (!entry.isDirectory() || !/^10\./.test(entry.name)) continue
      const candidate = join(bin, entry.name, arch, 'signtool.exe')
      if (existsSync(candidate)) matches.push({ version: entry.name, path: candidate })
    }
  }
  matches.sort((a, b) => b.version.localeCompare(a.version, undefined, { numeric: true }))
  return matches[0]?.path ?? null
}

export function subjectCN(subject) {
  const match = subject.includes('/')
    ? subject.match(/(?:^|\/)CN=([^/]+)/)
    : subject.match(/(?:^|,)\s*CN=([^,]+)/)
  return (match?.[1] ?? subject).trim()
}

function normalizeSubject(value) {
  const trimmed = value.trim()
  if (!trimmed) return defaultSubject
  return trimmed.startsWith('/') ? trimmed : `/${trimmed.replaceAll(',', '/')}`
}

function requireOpenSSL() {
  const openssl = findOpenSSL()
  if (!openssl) {
    throw new Error('openssl was not found; install OpenSSL or Git for Windows, or set DSH_MYGO_OPENSSL')
  }
  return openssl
}

function findOnPath(name) {
  const extensions = process.platform === 'win32' ? ['.exe', '.cmd', '.bat', ''] : ['']
  const entries = (process.env.PATH ?? '').split(process.platform === 'win32' ? ';' : ':')
  for (const entry of entries) {
    const dir = entry.replace(/^"|"$/g, '')
    if (!dir) continue
    for (const extension of extensions) {
      const candidate = join(dir, name + extension)
      if (existsSync(candidate)) return candidate
    }
  }
  return null
}

function findCertutil() {
  if (process.platform !== 'win32') return null
  const fromPath = findOnPath('certutil')
  if (fromPath) return fromPath
  const candidate = join(process.env.SystemRoot ?? 'C:\\Windows', 'System32', 'certutil.exe')
  return existsSync(candidate) ? candidate : null
}

function fingerprint(openssl, algorithm) {
  const output = runOrThrow(
    openssl,
    ['x509', '-in', pemPath, '-noout', '-fingerprint', `-${algorithm}`],
    'openssl x509',
  ).stdout
  return (output.match(/Fingerprint=(.+)/i)?.[1]?.trim() ?? '').replaceAll(':', '').toLowerCase()
}

function readInteger(name, fallback, min, max) {
  const raw = process.env[name]
  if (raw === undefined || raw === '') return fallback
  const value = Number(raw)
  if (!Number.isInteger(value) || value < min || value > max) {
    throw new Error(`${name} must be an integer from ${min} to ${max}`)
  }
  return value
}

function warnOnce(key, message) {
  mkdirSync(signingDir, { recursive: true })
  const marker = join(signingDir, `.warn-${key}`)
  if (existsSync(marker)) return
  writeFileSync(marker, `${new Date().toISOString()}\n`)
  console.warn(message)
}

function runTool(name, args) {
  return spawnSync(name, args, { encoding: 'utf8', windowsHide: true })
}

function runOrThrow(name, args, label) {
  const result = runTool(name, args)
  if (result.error) throw new Error(`${label}: ${result.error.message}`)
  if (result.status !== 0) throw new Error(`${label} failed:\n${toolOutput(result)}`)
  return result
}

function toolOutput(result) {
  return [result.stdout, result.stderr].filter(Boolean).join('\n').trim()
}
