import { signFile } from './signing.mjs'

const file = process.argv[2]
if (!file) {
  console.error('Usage: node scripts/sign-windows.mjs <file>')
  console.error('This command is run by mygo build through windows.signCommand.')
  process.exit(2)
}

try {
  signFile(file)
} catch (error) {
  console.error(`sign-windows: ${error.message}`)
  process.exit(1)
}
