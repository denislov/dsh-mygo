// Private release entry. It boots the mygo profile through runProfile instead
// of the public dsh CLI so stdout carries only JSON-RPC frames.
import { resolve } from 'node:path'
import { loadLayeredEnv } from '@deepseek-ai/dsh-app-boot'
import { runProfile } from '@deepseek-ai/dsh/profile-boot'

const argv = process.argv.slice(2)
const profile = argv.shift() ?? 'mygo'
if (!profile || profile.startsWith('-')) {
  process.stderr.write('mygo-host: a profile name is required\n')
  process.exit(2)
}

const patchFiles = []
const args = []
for (let i = 0; i < argv.length; i += 1) {
  if (argv[i] === '--patch') {
    const path = argv[i + 1]
    if (!path) {
      process.stderr.write('mygo-host: --patch requires a path\n')
      process.exit(2)
    }
    patchFiles.push(resolve(path))
    i += 1
    continue
  }
  args.push(argv[i])
}

await runProfile({
  environment: loadLayeredEnv('dsh'),
  profile,
  patchFiles,
  args,
})
