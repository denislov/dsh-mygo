import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const runtime = process.argv.includes('--runtime')
const env = { ...process.env }

if (runtime) {
  const resources = resolve(root, 'resources')
  if (!existsSync(resolve(resources, 'runtime', 'mygo-host.mjs'))) {
    console.error(`Bundled runtime is not built: ${resources}`)
    console.error('Run: node scripts/build-runtime.mjs')
    process.exit(2)
  }
  env.DSH_MYGO_RESOURCES_DIR = resources
  console.log(`Bundled runtime: ${resources}`)
} else {
  const arg = process.argv[2] ?? process.env.DSH_MYGO_DSH_DIR
  if (!arg) {
    console.error('Usage: node scripts/test-real-host.mjs <dsh-checkout>')
    console.error('   or: node scripts/test-real-host.mjs --runtime')
    console.error('   or: DSH_MYGO_DSH_DIR=<dsh-checkout> node scripts/test-real-host.mjs')
    process.exit(2)
  }
  const dir = resolve(arg)
  const hasSource = existsSync(resolve(dir, 'apps', 'cli', 'src', 'bin.ts'))
  const hasBuild = existsSync(resolve(dir, 'apps', 'cli', 'lib', 'bin.js'))
  if (!hasSource && !hasBuild) {
    console.error(`Not a DeepSeek Harness checkout: ${dir}`)
    process.exit(2)
  }
  env.DSH_MYGO_DSH_DIR = dir
  console.log(`DSH checkout: ${dir}`)
}

const result = spawnSync(
  'go',
  ['test', './internal/app', '-run', 'TestAgainstMygo|TestMygoReal', '-count=1', '-v', '-timeout', '10m'],
  { stdio: 'inherit', env },
)
process.exit(result.status ?? 1)
