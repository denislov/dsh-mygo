import { Command } from 'commander'
import Schema from '@deepseek-ai/schemastery'
import { exitOnStdinEnd, parseCmdline } from '@deepseek-ai/dsh-cmdline'
import { routeConsoleToStderr } from './stdout-guard.mjs'

// Plugins loaded after this bundle may log through console.log/info while
// handling requests. stdout carries the JSON-RPC channel, so keep it clean.
routeConsoleToStderr()

export const name = 'mygo-app-startup'
export const inject = ['cmdlineArgs', 'appExit', 'appReady']
export const Config = Schema.object({ profile: Schema.string().default('mygo') })

export function apply(ctx, config) {
  const program = new Command()
    .name(`dsh --profile ${config.profile}`)
    .description('Serve the dsh-mygo native client over stdio JSON-RPC.')
    .action(() => {
      exitOnStdinEnd(ctx, 'mygo-app.stdin')
      ctx.provide('mygoAppStartup', { accepted: true })
    })
  parseCmdline(ctx, program)
}
