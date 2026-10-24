import { createHash } from 'node:crypto'
import { cp, mkdir, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises'
import { join, sep } from 'node:path'
import { create as createTar, list as listTar } from 'tar'

const MANIFEST = 'manifest.json'
const HOST_BUNDLE = 'host'
const ENTRY = 'mygo-host.mjs'
const DSH_PACKAGE = join('node_modules', '@deepseek-ai', 'dsh', 'package.json')
const NODE_CANDIDATES = [join('node', 'node.exe'), join('node', 'node'), join('node', 'bin', 'node')]

export function runtimeID(manifest) {
  return `dsh-${manifest.dsh}-node-${manifest.node}-host-${manifest.hostVersion}-${manifest.platform}-${manifest.arch}`
}

export function runtimeAssetName(manifest) {
  return `dsh-mygo-runtime-${manifest.dsh}-node-${manifest.node}-host-${manifest.hostVersion}-${manifest.platform}-${manifest.arch}.tgz`
}

export async function buildRuntimeBundle({ runtimeDir, resourcesDir, outDir, versions }) {
  const platform = versions.platform ?? 'win32'
  const arch = versions.arch ?? 'x64'
  const node = versions.nodeRuntime
  const hostVersion = versions.version
  const appMin = versions.appCompatibility?.min
  const appMaxExclusive = versions.appCompatibility?.maxExclusive
  if (!/^\d+\.\d+\.\d+$/.test(appMin ?? '') || !/^\d+\.\d+\.\d+$/.test(appMaxExclusive ?? '')) {
    throw new Error('versions.json must declare numeric appCompatibility.min/maxExclusive')
  }

  await validateNode(runtimeDir)
  const dshPackage = JSON.parse(await readFile(join(runtimeDir, DSH_PACKAGE), 'utf8'))
  if (dshPackage.version !== versions.dsh) {
    throw new Error(`installed DSH ${dshPackage.version} does not match versions.json ${versions.dsh}`)
  }
  await validateHostBundle(join(resourcesDir, HOST_BUNDLE), versions)

  await rm(join(runtimeDir, HOST_BUNDLE), { recursive: true, force: true })
  await cp(join(resourcesDir, HOST_BUNDLE), join(runtimeDir, HOST_BUNDLE), { recursive: true })
  const manifest = {
    schemaVersion: 1,
    kind: 'dsh-runtime',
    id: '',
    dsh: dshPackage.version,
    node,
    platform,
    arch,
    protocolVersion: versions.protocolVersion,
    hostVersion,
    appMin,
    appMaxExclusive,
    entry: ENTRY,
    hostBundle: HOST_BUNDLE,
  }
  manifest.id = runtimeID(manifest)
  await writeFile(join(runtimeDir, MANIFEST), `${JSON.stringify(manifest, null, 2)}\n`)

  await mkdir(outDir, { recursive: true })
  const asset = runtimeAssetName(manifest)
  const assetPath = join(outDir, asset)
  const entries = await orderedEntries(runtimeDir)
  await createTar({
    cwd: runtimeDir,
    file: assetPath,
    gzip: true,
    portable: true,
    mtime: new Date(0),
    noDirRecurse: true,
    jobs: 1,
    onWriteEntry: (entry) => {
      entry.uid = 0
      entry.gid = 0
      entry.uname = ''
      entry.gname = ''
      entry.mode = entry.type === 'Directory' ? 0o755 : entry.path === 'node/node.exe' ? 0o755 : 0o644
    },
  }, entries)

  const bytes = await readFile(assetPath)
  const sha256 = createHash('sha256').update(bytes).digest('hex')
  const catalog = {
    schemaVersion: 1,
    runtimes: [{
      id: manifest.id,
      dsh: manifest.dsh,
      node: manifest.node,
      platform: manifest.platform,
      arch: manifest.arch,
      protocolVersion: manifest.protocolVersion,
      hostVersion: manifest.hostVersion,
      appMin: manifest.appMin,
      appMaxExclusive: manifest.appMaxExclusive,
      asset,
      sha256,
      size: bytes.length,
    }],
  }
  await writeFile(join(outDir, 'runtime-catalog.json'), `${JSON.stringify(catalog, null, 2)}\n`)
  await verifyRuntimeBundle(assetPath)
  return { manifest, assetPath, catalogPath: join(outDir, 'runtime-catalog.json'), sha256, size: bytes.length }
}

async function validateNode(runtimeDir) {
  for (const candidate of NODE_CANDIDATES) {
    try {
      const info = await stat(join(runtimeDir, candidate))
      if (info.isFile()) return
    } catch {
      // Try the next supported layout.
    }
  }
  throw new Error(`runtime is missing Node under ${join(runtimeDir, 'node')}`)
}

async function validateHostBundle(hostDir, versions) {
  const host = JSON.parse(await readFile(join(hostDir, 'package.json'), 'utf8'))
  if (host.name !== '@dsh-mygo/host' || typeof host.version !== 'string' || host.version === '') {
    throw new Error('resources/host is not a valid @dsh-mygo/host bundle')
  }
  if (host.version !== versions.version) {
    throw new Error(`Host bundle version ${host.version} does not match versions.json ${versions.version}`)
  }
  if (host.engines?.dsh !== versions.dsh) {
    throw new Error(`Host bundle pins DSH ${host.engines?.dsh}, want ${versions.dsh}`)
  }
  if (host.dshMygo?.protocolVersion !== versions.protocolVersion) {
    throw new Error('Host bundle protocolVersion does not match versions.json')
  }
  const compatibility = host.dshMygo?.appCompatibility
  if (compatibility?.min !== versions.appCompatibility.min || compatibility?.maxExclusive !== versions.appCompatibility.maxExclusive) {
    throw new Error('Host bundle appCompatibility does not match versions.json')
  }
  return host
}

async function orderedEntries(root) {
  const entries = []
  async function walk(directory, prefix) {
    const names = (await readdir(directory)).sort()
    for (const name of names) {
      const path = join(directory, name)
      const relativePath = prefix === '' ? name : `${prefix}/${name}`
      if (relativePath === 'go.mod') continue
      const info = await stat(path)
      entries.push(relativePath)
      if (info.isDirectory()) await walk(path, relativePath)
    }
  }
  await walk(root, '')
  if (!entries.includes(MANIFEST)) throw new Error('runtime manifest is missing')
  return [MANIFEST, ...entries.filter((entry) => entry !== MANIFEST)]
}

export async function verifyRuntimeBundle(assetPath) {
  const names = []
  await listTar({
    file: assetPath,
    onReadEntry: (entry) => {
      names.push(entry.path.replaceAll(sep, '/'))
    },
  })
  if (names[0] !== MANIFEST) throw new Error('runtime archive must start with manifest.json')
  if (names.includes('go.mod')) throw new Error('runtime archive must not contain go.mod')
  for (const required of [ENTRY, HOST_BUNDLE, 'node', 'node_modules']) {
    if (!names.some((name) => name === required || name.startsWith(`${required}/`))) {
      throw new Error(`runtime archive is missing ${required}`)
    }
  }
  return names
}
