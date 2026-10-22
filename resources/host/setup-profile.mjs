import { lstat, mkdir, mkdtemp, readFile, rename, symlink, writeFile } from 'node:fs/promises'
import { dirname, isAbsolute, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseArgs } from 'node:util'

const { values } = parseArgs({ options: { 'dsh-home': { type: 'string' } } })
if (!values['dsh-home'] || !isAbsolute(values['dsh-home'])) {
  throw new Error('Usage: node resources/host/setup-profile.mjs --dsh-home <absolute-directory>')
}
const bundle = dirname(fileURLToPath(import.meta.url))
const identity = JSON.parse(await readFile(join(bundle, 'package.json'), 'utf8'))
const profiles = join(values['dsh-home'], 'profiles')
const target = join(profiles, 'mygo')
await mkdir(profiles, { recursive: true })
try {
  await lstat(target)
  throw new Error('The mygo profile already exists; use dsh plugin to manage it')
} catch (error) {
  if (error.code !== 'ENOENT') throw error
}
const staging = await mkdtemp(join(profiles, '.mygo-'))
await mkdir(join(staging, 'node_modules', '@dsh-mygo'), { recursive: true })
await symlink(bundle, join(staging, 'node_modules', '@dsh-mygo', 'host'), process.platform === 'win32' ? 'junction' : 'dir')
await writeFile(join(staging, 'package.json'), `${JSON.stringify({
  name: 'dsh-profile-mygo', private: true, type: 'module',
  dependencies: { [identity.name]: `link:${bundle.replaceAll('\\', '/')}` },
  dsh: { profile: { bundles: ['@deepseek-ai/dsh-base', identity.name] } },
}, null, 2)}\n`)
await writeFile(join(staging, 'cordis.patch.yml'), '[]\n')
await rename(staging, target)
process.stdout.write(`Created mygo profile at ${target}\n`)
