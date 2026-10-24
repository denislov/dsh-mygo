import { existsSync } from 'node:fs'
import { resolve } from 'node:path'
import { hasAuthenticodeSignature, readSubjectCN, verifyFile, windowsReleaseFiles } from './signing.mjs'

const args = process.argv.slice(2)
const requireTrusted = args.includes('--require-trusted')
const help = args.includes('--help') || args.includes('-h')
const files = args.filter((arg) => !arg.startsWith('-'))

if (help) {
  console.log(`Usage: node scripts/verify-signatures.mjs [options] [file...]

Without files, verifies build/windows-amd64/dsh-mygo.exe and the installer.
A self-signed signature is reported as signed but untrusted unless the
certificate is in the current user's trust stores.

Options:
  --require-trusted  Fail when the certificate is not trusted on this machine
  -h, --help         Show this help`)
  process.exit(0)
}

if (files.length === 0) {
  const [exe, installer] = windowsReleaseFiles()
  if (!existsSync(exe)) {
    console.error(`Missing ${exe}; run: go tool mygo build`)
    process.exit(2)
  }
  if (!existsSync(installer)) {
    console.error(`Missing installer ${installer}; run: go tool mygo build`)
    process.exit(2)
  }
  files.push(exe, installer)
}

const expected = readSubjectCN()
let failed = 0
for (const file of files.map((entry) => resolve(entry))) {
  if (!existsSync(file)) {
    console.error(`✗ ${file}: missing`)
    failed++
    continue
  }
  if (!hasAuthenticodeSignature(file)) {
    console.error(`✗ ${file}: not Authenticode-signed`)
    failed++
    continue
  }
  const result = verifyFile(file)
  if (!result.ok) {
    console.error(`✗ ${file}: ${result.reason}`)
    if (result.detail) console.error(result.detail)
    failed++
    continue
  }
  const publisher = result.subjects?.find((value) => value.includes(expected))
  if (!publisher) {
    console.error(`✗ ${file}: signed by unexpected publisher ${result.subject || '(unknown)'}, want ${expected}`)
    failed++
    continue
  }
  if (result.trusted) {
    console.log(`✓ ${file}: signed by ${publisher}`)
  } else if (requireTrusted) {
    console.error(`✗ ${file}: signed by ${publisher}, but the certificate is not trusted on this machine`)
    console.error('  Run: node scripts/self-sign.mjs --trust')
    failed++
  } else {
    console.log(`✓ ${file}: signed by ${publisher} (self-signed; not trusted on this machine)`)
  }
}

process.exit(failed === 0 ? 0 : 1)
