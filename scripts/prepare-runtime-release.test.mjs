import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdtemp, mkdir, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'
import { create as createTar } from 'tar'
import { prepareRuntimeRelease } from './prepare-runtime-release.mjs'

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), 'dsh-runtime-release-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const versions = {
    version: '0.1.0',
    protocolVersion: 1,
    dsh: '0.2.1-rc.2',
    nodeRuntime: '24.14.0',
    repository: 'denislov/dsh-mygo',
    appCompatibility: { min: '0.1.0', maxExclusive: '0.2.0' },
  }
  await writeFile(join(root, 'versions.json'), JSON.stringify(versions))
  const staging = join(root, 'build/runtime/staging')
  const files = {
    'manifest.json': JSON.stringify({
      schemaVersion: 1, kind: 'dsh-runtime', id: 'dsh-test', dsh: versions.dsh, node: versions.nodeRuntime,
      platform: 'win32', arch: 'x64', protocolVersion: 1, hostVersion: versions.version,
      appMin: versions.appCompatibility.min, appMaxExclusive: versions.appCompatibility.maxExclusive,
      entry: 'mygo-host.mjs', hostBundle: 'host',
    }),
    'mygo-host.mjs': '// entry\n',
    'host/package.json': '{"name":"@dsh-mygo/host","version":"0.1.0"}\n',
    'node/node.exe': 'fake node',
    'node_modules/@deepseek-ai/dsh/package.json': '{"version":"0.2.1-rc.2"}\n',
  }
  for (const [path, content] of Object.entries(files)) {
    await mkdir(join(staging, path, '..'), { recursive: true })
    await writeFile(join(staging, path), content)
  }
  const asset = 'dsh-mygo-runtime-0.2.1-rc.2-node-24.14.0-host-0.1.0-win32-x64.tgz'
  await createTar({
    cwd: staging,
    file: join(root, 'build/runtime', asset),
    gzip: true,
    portable: true,
    mtime: new Date(0),
  }, Object.keys(files))
  const bytes = await readFile(join(root, 'build/runtime', asset))
  const entry = {
    id: 'dsh-0.2.1-rc.2-node-24.14.0-host-0.1.0-win32-x64',
    dsh: versions.dsh,
    node: versions.nodeRuntime,
    platform: 'win32',
    arch: 'x64',
    protocolVersion: 1,
    hostVersion: versions.version,
    appMin: versions.appCompatibility.min,
    appMaxExclusive: versions.appCompatibility.maxExclusive,
    asset,
    sha256: createHash('sha256').update(bytes).digest('hex'),
    size: bytes.length,
  }
  const catalog = { schemaVersion: 1, runtimes: [entry] }
  await writeFile(join(root, 'build/runtime/runtime-catalog.json'), JSON.stringify(catalog))
  return { root, versions, entry, catalog }
}

test('runtime release verifies its archive and emits notes and checksums', async t => {
  const { root, entry } = await fixture(t)
  const output = await prepareRuntimeRelease(root)
  const names = (await readdir(output)).sort()
  assert.deepEqual(names, [entry.asset, 'RUNTIME_NOTES.md', 'SHA256SUMS.txt', 'runtime-catalog.json'].sort())
  const notes = await readFile(join(output, 'RUNTIME_NOTES.md'), 'utf8')
  assert.match(notes, /App compatibility 0\.1\.0 <= app < 0\.2\.0/)
  assert.match(notes, new RegExp(entry.sha256))
  for (const line of (await readFile(join(output, 'SHA256SUMS.txt'), 'utf8')).trim().split('\n')) {
    const [digest, name] = line.split('  ')
    const bytes = await readFile(join(output, name))
    assert.equal(createHash('sha256').update(bytes).digest('hex'), digest)
    assert.equal((await stat(join(output, name))).isFile(), true)
  }
})

test('runtime release rejects checksum, platform and app range mismatches', async t => {
  const { root, catalog, entry } = await fixture(t)
  const path = join(root, 'build/runtime/runtime-catalog.json')
  await writeFile(path, JSON.stringify({ ...catalog, runtimes: [{ ...entry, sha256: '0'.repeat(64) }] }))
  await assert.rejects(prepareRuntimeRelease(root), /checksum/)
  await writeFile(path, JSON.stringify({ ...catalog, runtimes: [{ ...entry, platform: 'darwin' }] }))
  await assert.rejects(prepareRuntimeRelease(root), /Windows x64/)
  await writeFile(path, JSON.stringify({ ...catalog, runtimes: [{ ...entry, appMaxExclusive: '0.3.0' }] }))
  await assert.rejects(prepareRuntimeRelease(root), /compatibility/)
})
