import { spawn, spawnSync } from 'node:child_process'
import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appDir = join(root, 'build', 'windows-amd64')
const versions = JSON.parse(readFileSync(join(root, 'versions.json'), 'utf8'))
const installerPath = join(appDir, `dsh-mygo Setup ${versions.version} amd64.exe`)
const runtimeDir = resolve(process.env.DSH_MYGO_RUNTIME_DIR ?? join(root, 'runtime'))
const keep = process.argv.includes('--keep')

if (process.argv.includes('--help') || process.argv.includes('-h')) {
  usage()
  process.exit(0)
}
if (process.platform !== 'win32') {
  console.error('smoke-installer currently targets Windows x64')
  process.exit(2)
}
if (!existsSync(installerPath)) {
  console.error(`Missing ${installerPath}`)
  console.error('Run: go tool mygo build')
  process.exit(2)
}
if (!existsSync(join(runtimeDir, 'manifest.json')) || !existsSync(join(runtimeDir, 'mygo-host.mjs'))) {
  console.error(`Runtime bundle is missing at ${runtimeDir}`)
  console.error('Run: node scripts/build-runtime.mjs')
  process.exit(2)
}

const uninstallKey = 'HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\com.denislov.dshmygo'
const startMenu = join(
  process.env.APPDATA ?? '',
  'Microsoft',
  'Windows',
  'Start Menu',
  'Programs',
  'dsh-mygo.lnk',
)
const defaultInstall = join(process.env.LOCALAPPDATA ?? '', 'Programs', 'dsh-mygo')
const existing = []
if (regExists()) existing.push('the HKCU uninstall key')
if (existsSync(startMenu)) existing.push(startMenu)
if (existsSync(defaultInstall)) existing.push(defaultInstall)
if (existing.length > 0) {
  console.error('dsh-mygo already appears to be installed:')
  for (const entry of existing) console.error(`  ${entry}`)
  console.error('This smoke creates and removes the real Start Menu shortcut and HKCU uninstall key.')
  console.error('Run it on a clean Windows VM, or uninstall dsh-mygo first.')
  process.exit(2)
}

const work = join(root, '.tmp', `installer-smoke-${process.pid}-${Date.now()}`)
const installDir = join(work, 'app')
const dshHome = join(work, 'dsh-home')
const logs = join(work, 'logs')
const setupLogs = join(work, 'setup-logs')
const setupRuntimeHome = join(work, 'runtime-home')
mkdirSync(dshHome, { recursive: true })
writeFileSync(join(dshHome, 'keep.txt'), 'keep\n')

let failures = 0
let child
let exited = false
let appOutput = ''
let createdRegistry = false
let createdShortcut = false

const pass = (message) => console.log(`✓ ${message}`)
const fail = (message) => {
  console.error(`✗ ${message}`)
  failures++
}

try {
  console.log(`Installing ${installerPath}`)
  const install = spawnSync(installerPath, ['/S', `/D=${installDir}`], {
    cwd: work,
    encoding: 'utf8',
    windowsHide: true,
    timeout: 300000,
  })
  createdRegistry = regExists()
  createdShortcut = existsSync(startMenu)
  if (install.error) throw new Error(`installer failed: ${install.error.message}`)
  if (install.status !== 0) throw new Error(`installer exited ${install.status}\n${commandOutput(install)}`)
  pass('installer completed')

  for (const relative of [
    'dsh-mygo.exe',
    'Uninstall.exe',
    join('host', 'package.json'),
  ]) {
    if (existsSync(join(installDir, relative))) pass(`installed ${relative}`)
    else fail(`missing ${relative}`)
  }
  if (existsSync(join(installDir, 'runtime'))) fail('installed app must not contain the separate DSH runtime')
  else pass('runtime is not bundled in the app installer')
  if (createdShortcut) pass('created the Start Menu shortcut')
  else fail('missing Start Menu shortcut')
  if (createdRegistry) pass('created the HKCU uninstall key')
  else fail('missing HKCU uninstall key')

  const verify = spawnSync(
    process.execPath,
    [join(root, 'scripts', 'verify-signatures.mjs'), join(installDir, 'dsh-mygo.exe'), join(installDir, 'Uninstall.exe')],
    { cwd: root, stdio: 'inherit' },
  )
  if (verify.status === 0) pass('installed exe and Uninstall.exe are Authenticode-signed')
  else fail('installed exe or Uninstall.exe is not signed')

  const start = args => {
    child = spawn(join(installDir, 'dsh-mygo.exe'), args, {
      cwd: work,
      env: { ...process.env, PATH: '' },
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    exited = false
    child.stdout.on('data', (chunk) => {
      appOutput += chunk
    })
    child.stderr.on('data', (chunk) => {
      appOutput += chunk
    })
    child.on('exit', () => {
      exited = true
    })
  }
  const stop = async () => {
    killTree(child.pid)
    try {
      await waitFor(() => exited, 10000, 'app exit')
    } catch {
      fail('installed app did not exit after taskkill')
    }
    await delay(1000)
  }

  start(['-runtime-home', setupRuntimeHome, '-dsh-home', dshHome, '-log-dir', setupLogs, '-ui-state-dir', join(work, 'ui-state-setup')])
  await waitFor(() => existsSync(join(setupLogs, 'dsh-mygo.log')), 30000, 'setup application log')
  await delay(3000)
  const setupLog = readFileSync(join(setupLogs, 'dsh-mygo.log'), 'utf8')
  if (setupLog.includes('app created:') && !setupLog.includes('host start:')) pass('installed app shows runtime setup without a runtime')
  else fail('installed app started a host without a selected runtime')
  if (!existsSync(join(dshHome, 'profiles', 'mygo', 'package.json'))) pass('runtime setup did not seed a profile')
  else fail('runtime setup seeded a profile')
  await stop()

  start(['-runtime-dir', runtimeDir, '-dsh-home', dshHome, '-log-dir', logs, '-ui-state-dir', join(work, 'ui-state')])
  await waitFor(() => existsSync(join(dshHome, 'profiles', 'mygo', 'package.json')), 30000, 'profile seed')
  pass('installed app seeded the mygo profile with an explicit runtime')
  await waitFor(() => hasSessionLog(dshHome), 90000, 'session log')
  pass('installed app created a session')
  await waitFor(
    () => existsSync(join(logs, 'dsh-mygo.log')) && readFileSync(join(logs, 'dsh-mygo.log'), 'utf8').includes('session ready:'),
    90000,
    'session-ready application log',
  )
  pass('installed app recorded the session')
  await stop()

  const uninstaller = join(installDir, 'Uninstall.exe')
  if (!existsSync(uninstaller)) {
    fail('Uninstall.exe is missing; cannot test uninstall')
  } else {
    const uninstall = spawnSync(uninstaller, ['/S'], {
      cwd: work,
      encoding: 'utf8',
      windowsHide: true,
      timeout: 120000,
    })
    if (uninstall.error) fail(`uninstaller failed: ${uninstall.error.message}`)
    else if (uninstall.status !== 0) fail(`uninstaller exited ${uninstall.status}\n${commandOutput(uninstall)}`)
    else pass('uninstaller completed')
    try {
      await waitFor(() => !existsSync(installDir), 60000, 'install directory removal')
      pass('uninstaller removed the install directory')
    } catch (error) {
      fail(error.message)
    }
    try {
      await waitFor(() => !regExists(), 30000, 'uninstall key removal')
      pass('uninstaller removed the HKCU uninstall key')
    } catch (error) {
      fail(error.message)
    }
    try {
      await waitFor(() => !existsSync(startMenu), 30000, 'Start Menu shortcut removal')
      pass('uninstaller removed the Start Menu shortcut')
    } catch (error) {
      fail(error.message)
    }
  }

  if (existsSync(join(dshHome, 'keep.txt')) && readFileSync(join(dshHome, 'keep.txt'), 'utf8').trim() === 'keep') {
    pass('$DSH_HOME was preserved')
  } else {
    fail('$DSH_HOME was removed or changed')
  }
  if (hasSessionLog(dshHome)) pass('session history was preserved')
  else fail('session history was removed')
} catch (error) {
  fail(error.message)
  if (appOutput.trim()) console.error(appOutput.trim())
} finally {
  if (child && !exited) {
    killTree(child.pid)
    await waitFor(() => exited, 10000, 'app exit').catch(() => {})
  }
  // The preflight proved these did not exist before this smoke, so anything
  // still here is ours to remove even when the install failed halfway.
  if (createdRegistry && regExists()) {
    spawnSync('reg', ['delete', uninstallKey, '/f'], { windowsHide: true, stdio: 'ignore' })
  }
  if (createdShortcut && existsSync(startMenu)) {
    try {
      rmSync(startMenu, { force: true })
    } catch {
      // Explorer may hold the shortcut for a moment; the uninstaller already
      // retried, and this cleanup is best-effort.
    }
  }
  if (keep) {
    console.log(`Kept ${work}`)
  } else {
    try {
      rmSync(work, { recursive: true, force: true, maxRetries: 5, retryDelay: 500 })
    } catch (error) {
      console.error(`Could not remove ${work}: ${error.message}`)
    }
  }
}

process.exit(failures === 0 ? 0 : 1)

function regExists() {
  return spawnSync('reg', ['query', uninstallKey], { windowsHide: true, stdio: 'ignore' }).status === 0
}

function killTree(pid) {
  if (!pid) return
  spawnSync('taskkill', ['/PID', String(pid), '/T', '/F'], { windowsHide: true, stdio: 'ignore' })
}

function hasSessionLog(directory) {
  try {
    return readdirSync(directory, { recursive: true }).some((name) => String(name).endsWith('.jsonl.zstd'))
  } catch {
    return false
  }
}

function commandOutput(result) {
  return [result.stdout, result.stderr].filter(Boolean).join('\n').trim()
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

function usage() {
  console.log(`Usage: node scripts/smoke-installer.mjs [--keep]

Installs build/windows-amd64/dsh-mygo Setup <version> amd64.exe silently
into .tmp, verifies the app-only layout, checks the first-run runtime
setup, starts with an explicit runtime bundle, verifies exe/Uninstall.exe
signatures, uninstalls, and checks that $DSH_HOME survives.

This touches the real Start Menu shortcut and HKCU uninstall key, so it
refuses to run when dsh-mygo is already installed. Use a clean Windows VM
for release acceptance. --keep keeps the temporary directory for debugging.`)
}
