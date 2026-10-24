import { spawn, spawnSync } from 'node:child_process'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { windowsReleaseFiles } from './signing.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appDir = join(root, 'build', 'windows-amd64')
const [exe, installerPath] = windowsReleaseFiles(root)

if (process.platform !== 'win32') {
  console.error('smoke-packaged currently targets the Windows x64 package')
  process.exit(2)
}

const versions = JSON.parse(await readFile(join(root, 'versions.json'), 'utf8'))
if (!existsSync(installerPath)) {
  console.error(`Packaged installer is missing in ${appDir}`)
  console.error('Run: go tool mygo build')
  process.exit(2)
}
const required = [
  exe,
  installerPath,
  join(appDir, 'host', 'package.json'),
]
for (const path of required) {
  if (!existsSync(path)) {
    console.error(`Packaged app is incomplete: missing ${path}`)
    console.error('Run: go tool mygo build')
    process.exit(2)
  }
}
if (existsSync(join(appDir, 'runtime'))) {
  console.error('Packaged app must not contain the separate DSH runtime')
  process.exit(2)
}

const host = JSON.parse(await readFile(join(appDir, 'host', 'package.json'), 'utf8'))
if (host.version !== versions.version || host.engines?.dsh !== versions.dsh) {
  console.error(`Host bundle mismatch: ${JSON.stringify(host)}`)
  process.exit(2)
}

let failures = 0
const pass = (message) => console.log(`✓ ${message}`)
const fail = (message) => {
  console.error(`✗ ${message}`)
  failures++
}

if (process.env.DSH_MYGO_REQUIRE_SIGNATURE !== '0') {
  const checked = spawnSync(process.execPath, [join(root, 'scripts', 'verify-signatures.mjs'), exe, installerPath], {
    cwd: root,
    stdio: 'inherit',
  })
  if (checked.status === 0) pass('packaged artifacts are Authenticode-signed')
  else fail('packaged artifact signatures')
} else {
  console.log('- signature check skipped (DSH_MYGO_REQUIRE_SIGNATURE=0)')
}

const suite = spawnSync(
  'go',
  ['test', './internal/app', '-run', 'TestAgainstMygoRealComposition|TestAgainstMygoRestart|TestAgainstMygoProfileUpgradeAndSecondStart', '-count=1', '-timeout', '5m'],
  {
    cwd: root,
    stdio: 'inherit',
    env: { ...process.env, DSH_MYGO_RESOURCES_DIR: appDir },
  },
)
if (suite.status === 0) pass('packaged resources: composition + restart')
else fail('packaged resources real suite')

const work = await mkdtemp(join(tmpdir(), 'dsh-mygo-smoke-'))
const dshHome = join(work, 'dsh-home')
const logs = join(work, 'logs')
const child = spawn(exe, ['-dsh-home', dshHome, '-log-dir', logs, '-ui-state-dir', join(work, 'ui-state')], {
  env: { ...process.env, PATH: '' },
  stdio: ['ignore', 'pipe', 'pipe'],
})
let output = ''
child.stdout.on('data', (chunk) => {
  output += chunk
})
child.stderr.on('data', (chunk) => {
  output += chunk
})
let exited = false
let hostPid = 0
child.on('exit', () => {
  exited = true
})

try {
  await waitFor(() => existsSync(join(dshHome, 'profiles', 'mygo', 'package.json')), 30000, 'profile seed')
  pass('first run created the mygo profile')
  await waitFor(() => hasSessionLog(dshHome), 30000, 'session log')
  pass('bundled host created a session')
  const logPath = join(logs, 'dsh-mygo.log')
  await waitFor(() => existsSync(logPath) && readFileSync(logPath, 'utf8').includes('session ready:'), 30000, 'session-ready application log')
  pass('application log recorded the session')
  if (exited) {
    fail('application exited before the process-tree check')
  } else {
    hostPid = readHostPid(logPath)
    if (hostPid) pass(`found the host process (pid ${hostPid})`)
    else fail('could not read the host pid from the application log')
  }
} catch (error) {
  fail(error.message)
  if (output.trim()) console.error(output.trim())
} finally {
  if (!exited) {
    child.kill()
    await waitFor(() => exited, 10000, 'app exit').catch(() => {})
  }
  if (hostPid) {
    try {
      await waitFor(() => !processAlive(hostPid), 10000, 'host exit after app kill')
      pass('host process tree exited with the app')
    } catch (error) {
      fail(error.message)
    }
  }
  await rm(work, { recursive: true, force: true })
}

process.exit(failures === 0 ? 0 : 1)

function hasSessionLog(directory) {
  try {
    return readdirSync(directory, { recursive: true }).some((name) => String(name).endsWith('.jsonl.zstd'))
  } catch {
    return false
  }
}

async function waitFor(check, timeout, label) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline) {
    if (check()) return
    await new Promise((resolve) => setTimeout(resolve, 200))
  }
  throw new Error(`Timed out waiting for ${label}`)
}

function readHostPid(logPath) {
  try {
    const match = readFileSync(logPath, 'utf8').match(/host started: pid=(\d+)/)
    return match ? Number(match[1]) : 0
  } catch {
    return 0
  }
}

function processAlive(pid) {
  const result = spawnSync(
    'powershell',
    ['-NoProfile', '-Command', `Get-Process -Id ${pid} -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Id`],
    { encoding: 'utf8', windowsHide: true },
  )
  return result.status === 0 && result.stdout.trim() !== ''
}
