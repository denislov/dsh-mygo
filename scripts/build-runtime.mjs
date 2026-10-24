import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { existsSync } from 'node:fs'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { buildRuntimeBundle } from './runtime-bundle.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const runtimeDir = join(root, 'runtime')
const outputDir = join(root, 'build', 'runtime')
const versions = JSON.parse(await readFile(join(root, 'versions.json'), 'utf8'))

if (process.platform !== 'win32' || process.arch !== 'x64') {
  throw new Error('scripts/build-runtime.mjs currently supports Windows x64 only')
}

const nodeTarget = join(runtimeDir, 'node', 'node.exe')
if (!existsSync(nodeTarget) || process.argv.includes('--force-node')) {
  await downloadNode(versions.nodeRuntime, nodeTarget)
}

if (!process.argv.includes('--skip-npm-install')) {
  const install = spawnSync('npm ci --omit=dev --no-audit --no-fund', {
    cwd: runtimeDir,
    stdio: 'inherit',
    shell: true,
  })
  if (install.status !== 0) process.exit(install.status ?? 1)
}

const result = await buildRuntimeBundle({
  runtimeDir,
  resourcesDir: join(root, 'resources'),
  outDir: outputDir,
  versions,
})
console.log(`Runtime ${result.manifest.id}`)
console.log(`Archive: ${result.assetPath} (${result.size} bytes)`)
console.log(`SHA256: ${result.sha256}`)
console.log(`Catalog: ${result.catalogPath}`)

async function downloadNode(version, target) {
  const base = `https://nodejs.org/dist/v${version}`
  console.log(`Downloading Node ${version}...`)
  const shasums = await fetchText(`${base}/SHASUMS256.txt`)
  const line = shasums.split('\n').find((entry) => entry.trim().endsWith('win-x64/node.exe'))
  if (!line) throw new Error(`No win-x64/node.exe checksum for Node ${version}`)
  const expected = line.trim().split(/\s+/)[0]
  const response = await fetch(`${base}/win-x64/node.exe`)
  if (!response.ok) throw new Error(`Download failed: ${response.status} ${response.statusText}`)
  const bytes = Buffer.from(await response.arrayBuffer())
  const actual = createHash('sha256').update(bytes).digest('hex')
  if (actual !== expected) throw new Error(`Node checksum mismatch: got ${actual}, want ${expected}`)
  await mkdir(dirname(target), { recursive: true })
  await writeFile(target, bytes, { mode: 0o755 })
  console.log(`Node ${version} ready: ${target}`)
}

async function fetchText(url) {
  const response = await fetch(url)
  if (!response.ok) throw new Error(`Fetch failed: ${url} ${response.status}`)
  return response.text()
}
