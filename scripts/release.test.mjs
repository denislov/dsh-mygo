import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createHash } from 'node:crypto'
import test from 'node:test'
import { prepareRelease } from './prepare-release.mjs'

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), 'dsh-release-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const files = {
    'versions.json': JSON.stringify({
      version: '0.1.0', protocolVersion: 1, dsh: 'test', nodeRuntime: '24.14.0',
      repository: 'denislov/dsh-mygo',
      appCompatibility: { min: '0.1.0', maxExclusive: '0.2.0' },
    }),
    'build/windows-amd64/host/package.json': JSON.stringify({
      version: '0.1.0',
      engines: { node: '>=24', dsh: 'test' },
      dshMygo: { protocolVersion: 1, appCompatibility: { min: '0.1.0', maxExclusive: '0.2.0' } },
    }),
    'build/windows-amd64/dsh-mygo Setup 0.1.0 amd64.exe': 'installer-test-bytes',
    '.signing/dsh-mygo.cer': 'public-certificate-test-bytes',
    '.signing/dsh-mygo.pfx': 'private-marker',
    '.signing/password.txt': 'private-marker',
    '.env': 'private-marker',
    'docs/getting-started.md': '# Getting started\n',
  }
  for (const [path, content] of Object.entries(files)) {
    await mkdir(join(root, path, '..'), { recursive: true })
    await writeFile(join(root, path), content)
  }
  return root
}

test('release assets use an exact allowlist and checksums match all attachments', async t => {
  const root = await fixture(t)
  const output = await prepareRelease(root)
  const names = (await readdir(output)).sort()
  assert.deepEqual(names, ['RELEASE_NOTES.md', 'SHA256SUMS.txt', 'START_HERE.zh-CN.md', 'dsh-mygo-0.1.0-windows-x64-setup.exe', 'dsh-mygo-signing.cer'].sort())
  for (const line of (await readFile(join(output, 'SHA256SUMS.txt'), 'utf8')).trim().split('\n')) {
    const [digest, name] = line.split('  ')
    const bytes = await readFile(join(output, name))
    assert.equal(createHash('sha256').update(bytes).digest('hex'), digest)
    assert.ok(!bytes.toString().includes('private-marker'))
  }
})

test('release preparation rejects an installer that embeds a runtime', async t => {
  const root = await fixture(t)
  await mkdir(join(root, 'build/windows-amd64/runtime'), { recursive: true })
  await writeFile(join(root, 'build/windows-amd64/runtime/manifest.json'), '{}')
  await assert.rejects(prepareRelease(root), /must not contain/)
})

test('release preparation refuses mismatched Host identity and unexpected assets', async t => {
  const root = await fixture(t)
  await writeFile(join(root, 'build/windows-amd64/host/package.json'), '{"version":"other"}')
  await assert.rejects(prepareRelease(root), /identity/)
  await writeFile(join(root, 'build/windows-amd64/host/package.json'), JSON.stringify({
    version: '0.1.0',
    engines: { node: '>=24', dsh: 'test' },
    dshMygo: { protocolVersion: 1, appCompatibility: { min: '0.1.0', maxExclusive: '0.2.0' } },
  }))
  await mkdir(join(root, 'build/release/v0.1.0'), { recursive: true })
  await writeFile(join(root, 'build/release/v0.1.0/unexpected.pfx'), 'private-marker')
  await assert.rejects(prepareRelease(root), /Unexpected/)
})
