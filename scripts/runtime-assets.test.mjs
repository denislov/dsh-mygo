import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'
import { prepareRuntimeAssets, verifyRuntimeAssets } from './runtime-assets.mjs'

test('runtime assets survive the MyGo dot-file filter without changing model data', async t => {
  const root = await mkdtemp(join(tmpdir(), 'mygo-runtime-assets-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const providers = join(root, 'node_modules/@earendil-works/pi-ai/dist/providers')
  await mkdir(join(providers, 'data'), { recursive: true })
  const original = 'import models from "./data/.manifest.json" with { type: "json" };\nexport default models\n'
  const manifest = '{"models":["example"]}\n'
  await writeFile(join(providers, 'all.js'), original)
  await writeFile(join(providers, 'data/.manifest.json'), manifest)
  await prepareRuntimeAssets(root)
  await prepareRuntimeAssets(root)
  await verifyRuntimeAssets(root)
  assert.equal(await readFile(join(providers, 'data/models-manifest.json'), 'utf8'), manifest)
  assert.ok(!(await readFile(join(providers, 'all.js'), 'utf8')).includes('./data/.manifest.json'))
  assert.ok((await readdir(join(providers, 'data'))).filter(name => !name.startsWith('.')).includes('models-manifest.json'))
  await writeFile(join(providers, 'all.js'), 'unexpected dependency layout')
  await assert.rejects(prepareRuntimeAssets(root), /Unsupported pi-ai asset layout/)
})
