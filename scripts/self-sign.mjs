import { certificateInfo, defaultSubject, ensureCertificate, removeTrust, trustCertificate } from './signing.mjs'

const args = new Set(process.argv.slice(2))
const known = new Set(['--force', '--trust', '--remove-trust', '--quiet', '--help', '-h'])

if (args.has('--help') || args.has('-h')) {
  usage()
  process.exit(0)
}
for (const arg of args) {
  if (!known.has(arg)) {
    console.error(`Unknown option: ${arg}`)
    usage()
    process.exit(2)
  }
}

try {
  if (args.has('--remove-trust')) {
    removeTrust()
    process.exit(0)
  }

  ensureCertificate({ force: args.has('--force'), quiet: true })
  if (args.has('--trust')) trustCertificate()
  if (!args.has('--quiet')) {
    const info = certificateInfo()
    console.log('Self-signed code signing certificate ready')
    console.log(`  subject: ${info.subject}`)
    console.log(`  sha1:    ${info.sha1}`)
    console.log(`  sha256:  ${info.sha256}`)
    console.log(`  valid:   ${info.notBefore} to ${info.notAfter}`)
    console.log(`  pfx:     .signing/dsh-mygo.pfx`)
    console.log(`  cert:    .signing/dsh-mygo.cer`)
    console.log('')
    console.log('The private key stays in .signing/ and is never committed.')
    console.log('Windows will show "Unknown publisher" until the certificate is trusted on the target machine.')
    console.log('For local no-warning testing: node scripts/self-sign.mjs --trust')
    console.log('To remove that trust:          node scripts/self-sign.mjs --remove-trust')
    console.log('Publish the .cer and its SHA-256 fingerprint with releases so users can verify the signature.')
  }
} catch (error) {
  console.error(`self-sign: ${error.message}`)
  process.exit(1)
}

function usage() {
  console.log(`Usage: node scripts/self-sign.mjs [options]

Creates .signing/dsh-mygo.pfx and dsh-mygo.cer when needed, and reuses them
afterwards. Signing starts automatically on the next "go tool mygo build".

Options:
  --force          Replace the existing certificate. Earlier builds will no
                   longer verify against the new one.
  --trust          Trust the certificate for the current user, for local
                   no-warning tests. Adds it to Root and TrustedPublisher.
  --remove-trust   Remove the certificate from the current user's stores.
  --quiet          Do not print the certificate summary.
  -h, --help       Show this help.

Environment:
  DSH_MYGO_SIGN_SUBJECT   OpenSSL subject (default ${defaultSubject})
  DSH_MYGO_SIGN_DAYS      Certificate validity in days (default 1095)
  DSH_MYGO_SIGN_KEY_BITS  RSA key size (default 3072)
  DSH_MYGO_OPENSSL        Path to openssl when it is not on PATH`)
}
