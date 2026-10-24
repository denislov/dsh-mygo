import { copyFile, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

// MyGo v0.6.2 omits dot-named resources. Adapt this pinned dependency's
// generated asset reference in the build tree, without modifying DSH core.
export async function prepareRuntimeAssets(runtimeDir) {
  const providers = join(runtimeDir, 'node_modules/@earendil-works/pi-ai/dist/providers')
  const entry = join(providers, 'all.js')
  const original = './data/.manifest.json'
  const visible = './data/models-manifest.json'
  const source = await readFile(entry, 'utf8')
  if (!source.includes(`"${original}"`) && !source.includes(`"${visible}"`)) throw new Error('Unsupported pi-ai asset layout; review the pinned runtime before packaging')
  const data = await readFile(join(providers, 'data/.manifest.json'))
  JSON.parse(data.toString('utf8'))
  await copyFile(join(providers, 'data/.manifest.json'), join(providers, 'data/models-manifest.json'))
  await writeFile(entry, source.replace(`"${original}"`, `"${visible}"`))
  await verifyRuntimeAssets(runtimeDir)
}

export async function verifyRuntimeAssets(runtimeDir) {
  const providers = join(runtimeDir, 'node_modules/@earendil-works/pi-ai/dist/providers')
  const entry = await readFile(join(providers, 'all.js'), 'utf8')
  if (entry.includes('"./data/.manifest.json"') || !entry.includes('"./data/models-manifest.json"')) throw new Error('Packaged pi-ai still depends on a filtered dot-file')
  JSON.parse(await readFile(join(providers, 'data/models-manifest.json'), 'utf8'))
}
