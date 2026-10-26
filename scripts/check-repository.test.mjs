import assert from 'node:assert/strict'
import test from 'node:test'
import { findTrackedArtifacts } from './check-repository.mjs'

test('repository artifact check rejects generated and secret files', () => {
  const found = findTrackedArtifacts([
    'main.go',
    'runtime/package.json',
    'runtime/host/index.mjs',
    'runtime/node/bin/node',
    'runtime/manifest.json',
    'build/windows-amd64/dsh-mygo.exe',
    '.signing/local.pfx',
    'certs/release.pem',
    'dist/runtime.tgz',
    '.env',
  ])
  assert.deepEqual(found, ['runtime/host/index.mjs', 'runtime/node/bin/node', 'runtime/manifest.json', 'build/windows-amd64/dsh-mygo.exe', '.signing/local.pfx', 'certs/release.pem', 'dist/runtime.tgz', '.env'])
})
