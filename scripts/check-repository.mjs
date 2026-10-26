#!/usr/bin/env node
// Fails when generated artifacts, local signing material or build outputs are
// tracked. Runtime source stays tracked; only its generated/host payloads are
// forbidden.
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'

const generatedPrefixes = ['build/', '.signing/', 'node_modules/', 'runtime/node/', 'runtime/node_modules/', 'runtime/host/']
const generatedFiles = new Set(['runtime/manifest.json'])
const secretPattern = /\.(pfx|p12|pem|key)$/i
const archivePattern = /\.tgz$/i

export function findTrackedArtifacts(paths) {
  const found = []
  for (const path of paths) {
    if (generatedPrefixes.some((prefix) => path.startsWith(prefix)) || generatedFiles.has(path) || secretPattern.test(path) || archivePattern.test(path) || path === '.env') {
      found.push(path)
    }
  }
  return found
}

export function checkRepository(root = process.cwd()) {
  const tracked = execFileSync('git', ['ls-files', '-z'], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean)
  const artifacts = findTrackedArtifacts(tracked)
  if (artifacts.length > 0) {
    throw new Error(`generated or secret files are tracked:\n${artifacts.join('\n')}`)
  }
  const ignore = readFileSync(join(root, '.gitignore'), 'utf8')
  for (const entry of ['/build/', '/.signing/', '/node_modules/', '/runtime/node/', '/runtime/node_modules/', '/runtime/host/', '/runtime/manifest.json']) {
    if (!ignore.includes(entry)) throw new Error(`.gitignore is missing ${entry}`)
  }
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    checkRepository()
    console.log('repository artifact check passed')
  } catch (error) {
    console.error(error.message)
    process.exit(1)
  }
}
