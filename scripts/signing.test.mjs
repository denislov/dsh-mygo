import assert from 'node:assert/strict'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'
import { hasAuthenticodeSignature, subjectCN, windowsReleaseFiles } from './signing.mjs'

test('release verification selects the current version even when older installers remain', () => {
  const dir = mkdtempSync(join(tmpdir(), 'dsh-mygo-release-files-'))
  try {
    writeFileSync(join(dir, 'versions.json'), '{"version":"0.1.1"}')
    const files = windowsReleaseFiles(dir)
    assert.equal(files[0], join(dir, 'build/windows-amd64/dsh-mygo.exe'))
    assert.equal(files[1], join(dir, 'build/windows-amd64/dsh-mygo Setup 0.1.1 amd64.exe'))
  } finally { rmSync(dir, { recursive: true, force: true }) }
})

test('subjectCN reads slash and comma separated subjects', () => {
  assert.equal(subjectCN('/CN=dsh-mygo (self-signed)/O=dsh-mygo'), 'dsh-mygo (self-signed)')
  assert.equal(subjectCN('CN=dsh-mygo, O=dsh-mygo'), 'dsh-mygo')
})

test('hasAuthenticodeSignature reads the PE certificate table', () => {
  const dir = mkdtempSync(join(tmpdir(), 'dsh-mygo-signing-'))
  try {
    const unsigned = join(dir, 'unsigned.exe')
    writeFileSync(unsigned, peImage(0))
    assert.equal(hasAuthenticodeSignature(unsigned), false)

    const signed = join(dir, 'signed.exe')
    writeFileSync(signed, peImage(0x120))
    assert.equal(hasAuthenticodeSignature(signed), true)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('hasAuthenticodeSignature rejects files that are not PE images', () => {
  const dir = mkdtempSync(join(tmpdir(), 'dsh-mygo-signing-'))
  try {
    const text = join(dir, 'readme.txt')
    writeFileSync(text, 'not an executable')
    assert.equal(hasAuthenticodeSignature(text), false)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

// peImage builds the smallest PE32+ header that carries a certificate table.
function peImage(certificateSize) {
  const peOffset = 0x80
  const optionalSize = 240
  const buffer = Buffer.alloc(peOffset + 24 + optionalSize)
  buffer.write('MZ', 0)
  buffer.writeUInt32LE(peOffset, 0x3c)
  buffer.write('PE\0\0', peOffset, 'binary')
  buffer.writeUInt16LE(optionalSize, peOffset + 20)
  buffer.writeUInt16LE(0x20b, peOffset + 24)
  const security = peOffset + 24 + 112 + 4 * 8
  buffer.writeUInt32LE(certificateSize, security + 4)
  return buffer
}
