import { createHash } from 'node:crypto'
import { copyFile, mkdir, readFile, readdir, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { verifyRuntimeBundle } from './runtime-bundle.mjs'

const digest = (bytes) => createHash('sha256').update(bytes).digest('hex')

export async function prepareRuntimeRelease(root) {
  const versions = JSON.parse(await readFile(join(root, 'versions.json'), 'utf8'))
  const source = join(root, 'build', 'runtime')
  const catalogBytes = await readFile(join(source, 'runtime-catalog.json'))
  const catalog = JSON.parse(catalogBytes.toString('utf8'))
  if (catalog.schemaVersion !== 1 || !Array.isArray(catalog.runtimes) || catalog.runtimes.length === 0) {
    throw new Error('Invalid runtime catalog')
  }
  const entry = catalog.runtimes[0]
  if (entry.platform !== 'win32' || entry.arch !== 'x64') throw new Error('Runtime release is not Windows x64')
  if (entry.appMin !== versions.appCompatibility?.min || entry.appMaxExclusive !== versions.appCompatibility?.maxExclusive) {
    throw new Error('Runtime catalog app compatibility does not match versions.json')
  }
  const assetBytes = await readFile(join(source, entry.asset))
  if (assetBytes.length !== entry.size || digest(assetBytes) !== entry.sha256) {
    throw new Error('Runtime archive does not match the catalog checksum')
  }
  await verifyRuntimeBundle(join(source, entry.asset))

  const output = join(root, 'build', 'release', `runtime-${entry.id}`)
  const allowed = [entry.asset, 'runtime-catalog.json', 'SHA256SUMS.txt', 'RUNTIME_NOTES.md']
  await mkdir(output, { recursive: true })
  if ((await readdir(output)).some((name) => !allowed.includes(name))) {
    throw new Error('Unexpected file in runtime release output; use an empty release directory')
  }
  await copyFile(join(source, entry.asset), join(output, entry.asset))
  await writeFile(join(output, 'runtime-catalog.json'), catalogBytes)
  await writeFile(join(output, 'RUNTIME_NOTES.md'), `# dsh-mygo runtime ${entry.id}

- DSH ${entry.dsh}
- Node ${entry.node}
- Host bundle ${entry.hostVersion}
- Channel protocol ${entry.protocolVersion}
- App compatibility ${entry.appMin} <= app < ${entry.appMaxExclusive}
- Platform ${entry.platform}/${entry.arch}

This archive contains Node, the DSH production dependency closure, the private mygo-host entry and the matching @dsh-mygo/host bundle.
The app verifies SHA256 from runtime-catalog.json before installing. The archive is executable code and runs with the current user's permissions.

SHA256: \`${entry.sha256}\`
`)
  const names = [entry.asset, 'runtime-catalog.json', 'RUNTIME_NOTES.md']
  const lines = []
  for (const name of names) lines.push(`${digest(await readFile(join(output, name)))}  ${name}`)
  await writeFile(join(output, 'SHA256SUMS.txt'), `${lines.join('\n')}\n`)
  return output
}

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  console.log(`Runtime release assets ready: ${await prepareRuntimeRelease(root)}`)
}
