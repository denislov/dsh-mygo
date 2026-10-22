import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { resolve } from 'node:path'

const arg = process.argv[2] ?? process.env.DSH_MYGO_DSH_DIR
if (!arg) {
  console.error('Usage: node scripts/test-real-host.mjs <dsh-checkout>')
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

console.log(`DSH checkout: ${dir}`)
const result = spawnSync(
  'go',
  ['test', './internal/app', '-run', 'TestAgainstMygo|TestMygoReal', '-count=1', '-v', '-timeout', '10m'],
  {
    stdio: 'inherit',
    env: { ...process.env, DSH_MYGO_DSH_DIR: dir },
  },
)
process.exit(result.status ?? 1)
