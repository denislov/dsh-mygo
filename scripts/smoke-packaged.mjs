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
const runtimeDir = resolve(process.env.DSH_MYGO_RUNTIME_DIR ?? join(root, 'runtime'))

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
const required = [exe, installerPath, join(appDir, 'host', 'package.json')]
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

const runtimeManifest = join(runtimeDir, 'manifest.json')
if (!existsSync(runtimeManifest)) {
  console.error(`Runtime bundle is missing at ${runtimeDir}`)
  console.error('Run: node scripts/build-runtime.mjs')
  process.exit(2)
}
const manifest = JSON.parse(await readFile(runtimeManifest, 'utf8'))
if (manifest.dsh !== versions.dsh || manifest.hostVersion !== versions.version || manifest.protocolVersion !== versions.protocolVersion) {
  console.error(`Runtime bundle does not match versions.json: ${JSON.stringify(manifest)}`)
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

const work = await mkdtemp(join(tmpdir(), 'dsh-mygo-smoke-'))
const setupHome = join(work, 'setup-home')
const setupLogs = join(work, 'setup-logs')
const setupState = join(work, 'setup-state')
const runtimeHome = join(work, 'runtime-home')
const dshHome = join(work, 'dsh-home')
const logs = join(work, 'logs')
const logsAgain = join(work, 'logs-again')
const uiState = join(work, 'ui-state')

try {
  const first = await runApp(['-runtime-home', runtimeHome, '-dsh-home', setupHome, '-log-dir', setupLogs, '-ui-state-dir', setupState], setupLogs)
  try {
    await waitFor(() => existsSync(join(setupLogs, 'dsh-mygo.log')), 30000, 'setup application log')
    await delay(3000)
    const setupLog = readFileSync(join(setupLogs, 'dsh-mygo.log'), 'utf8')
    if (setupLog.includes('app created:') && !setupLog.includes('host start:')) pass('first launch stays in runtime setup')
    else fail('first launch started without a selected runtime')
    if (!existsSync(join(setupHome, 'profiles', 'mygo', 'package.json'))) pass('setup did not seed a profile')
    else fail('setup seeded a profile without a runtime')
    if (!hasSessionLog(setupHome)) pass('setup did not create a session')
    else fail('setup created a session without a runtime')
  } finally {
    await stopApp(first)
  }

  const second = await runApp(
    ['-runtime-dir', runtimeDir, '-dsh-home', dshHome, '-log-dir', logs, '-ui-state-dir', uiState],
    logs,
  )
  let hostPid = 0
  try {
    await waitFor(() => existsSync(join(dshHome, 'profiles', 'mygo', 'package.json')), 30000, 'profile seed')
    pass('explicit runtime seeded the mygo profile')
    await waitFor(() => hasSessionLog(dshHome), 60000, 'session log')
    pass('explicit runtime created a session')
    const logPath = join(logs, 'dsh-mygo.log')
    await waitFor(() => existsSync(logPath) && readFileSync(logPath, 'utf8').includes('session ready:'), 90000, 'session-ready application log')
    pass('application log recorded the session')
    if (second.child.exitCode !== null) fail('application exited before the process-tree check')
    else {
      hostPid = readHostPid(logPath)
      if (hostPid) pass(`found the host process (pid ${hostPid})`)
      else fail('could not read the host pid from the application log')
      await delay(1500)
    }
  } catch (error) {
    fail(error.message)
  } finally {
    await stopApp(second)
  }
  if (hostPid) {
    try {
      await waitFor(() => !processAlive(hostPid), 10000, 'host exit after app kill')
      pass('host process tree exited with the app')
    } catch (error) {
      fail(error.message)
    }
  }

  const resumedLogPath = join(logsAgain, 'dsh-mygo.log')
  const third = await runApp(
    ['-runtime-dir', runtimeDir, '-dsh-home', dshHome, '-log-dir', logsAgain, '-ui-state-dir', uiState],
    logsAgain,
  )
  try {
    await waitFor(() => existsSync(resumedLogPath) && readFileSync(resumedLogPath, 'utf8').includes('session ready:'), 90000, 'second launch session')
    const firstSession = readSessionID(join(logs, 'dsh-mygo.log'))
    const resumedSession = readSessionID(resumedLogPath)
    if (firstSession && firstSession === resumedSession) pass('second launch resumed the same session')
    else fail(`second launch did not resume the same session (${firstSession} -> ${resumedSession})`)
  } catch (error) {
    fail(error.message)
  } finally {
    await stopApp(third)
  }
} catch (error) {
  fail(error.message)
} finally {
  await rm(work, { recursive: true, force: true })
}

process.exit(failures === 0 ? 0 : 1)

function runApp(args, logDir) {
  const child = spawn(exe, args, {
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
  return Promise.resolve({ child, logDir, output: () => output })
}

async function stopApp(handle) {
  handle.child.kill()
  await waitFor(() => handle.child.exitCode !== null, 10000, 'app exit').catch(() => {
    spawnSync('taskkill', ['/PID', String(handle.child.pid), '/T', '/F'], { windowsHide: true, stdio: 'ignore' })
  })
}

function hasSessionLog(directory) {
  try {
    return readdirSync(directory, { recursive: true }).some((name) => String(name).endsWith('.jsonl.zstd'))
  } catch {
    return false
  }
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds))
}

async function waitFor(check, timeout, label) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline) {
    if (check()) return
    await delay(200)
  }
  throw new Error(`Timed out waiting for ${label}`)
}

function readSessionID(logPath) {
  try {
    const matches = [...readFileSync(logPath, 'utf8').matchAll(/session ready: ([A-Za-z0-9_-]+)/g)]
    return matches.length ? matches[matches.length - 1][1] : ''
  } catch {
    return ''
  }
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
