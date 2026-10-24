import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'
import { buildRuntimeBundle, runtimeAssetName, verifyRuntimeBundle } from './runtime-bundle.mjs'

const versions = {
  version: '0.1.1',
  protocolVersion: 1,
  dsh: 'test-dsh',
  nodeRuntime: '24.0.0',
  appCompatibility: { min: '0.1.1', maxExclusive: '0.2.0' },
}

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), 'dsh-runtime-bundle-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const runtimeDir = join(root, 'runtime')
  const resourcesDir = join(root, 'resources')
  const hostDir = join(resourcesDir, 'host')
  const files = {
    [join(runtimeDir, 'node', 'node.exe')]: 'fake-node',
    [join(runtimeDir, 'mygo-host.mjs')]: '// entry\n',
    [join(runtimeDir, 'go.mod')]: 'module ignored\n',
    [join(runtimeDir, 'node_modules', '@deepseek-ai', 'dsh', 'package.json')]: JSON.stringify({ name: '@deepseek-ai/dsh', version: 'test-dsh' }),
    [join(runtimeDir, 'node_modules', 'example', '.manifest.json')]: '{"hidden":true}',
    [join(hostDir, 'package.json')]: JSON.stringify({
      name: '@dsh-mygo/host',
      version: versions.version,
      engines: { node: '>=24', dsh: versions.dsh },
      dshMygo: { protocolVersion: versions.protocolVersion, appCompatibility: versions.appCompatibility },
    }),
    [join(hostDir, 'index.mjs')]: '// host\n',
  }
  for (const [path, content] of Object.entries(files)) {
    await mkdir(join(path, '..'), { recursive: true })
    await writeFile(path, content)
  }
  return { root, runtimeDir, resourcesDir, outDir: join(root, 'out') }
}

test('runtime bundles are deterministic, include hidden files and start with the manifest', async (t) => {
  const { runtimeDir, resourcesDir, outDir } = await fixture(t)
  const first = await buildRuntimeBundle({ runtimeDir, resourcesDir, outDir, versions })
  const firstNames = await verifyRuntimeBundle(first.assetPath)
  assert.equal(firstNames[0], 'manifest.json')
  assert.ok(!firstNames.includes('go.mod'))
  assert.ok(firstNames.includes('node/node.exe'))
  assert.ok(firstNames.includes('node_modules/example/.manifest.json'))
  assert.ok(firstNames.includes('host/package.json'))
  assert.equal(first.manifest.id, 'dsh-test-dsh-node-24.0.0-host-0.1.1-win32-x64')

  const catalog = JSON.parse(await readFile(first.catalogPath, 'utf8'))
  assert.equal(catalog.runtimes[0].id, first.manifest.id)
  assert.equal(catalog.runtimes[0].sha256, first.sha256)
  assert.equal(catalog.runtimes[0].size, first.size)
  assert.equal(catalog.runtimes[0].asset, runtimeAssetName(first.manifest))

  const second = await buildRuntimeBundle({ runtimeDir, resourcesDir, outDir: join(outDir, 'second'), versions })
  assert.equal(second.sha256, first.sha256)
  assert.equal(second.size, first.size)
})
