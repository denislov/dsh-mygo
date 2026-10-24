import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { copyFile, mkdir, readFile, readdir, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const digest = bytes => createHash('sha256').update(bytes).digest('hex')

// Only these named inputs become attachments. Never copy the signing directory
// or development environment wholesale into a distributable folder.
export async function prepareRelease(root) {
  const identity = JSON.parse(await readFile(join(root, 'versions.json'), 'utf8'))
  if (!/^\d+\.\d+\.\d+$/.test(identity.version)) throw new Error('Invalid release version')
  const app = join(root, 'build/windows-amd64')
  const runtime = JSON.parse(await readFile(join(app, 'runtime/manifest.json'), 'utf8'))
  const host = JSON.parse(await readFile(join(app, 'host/package.json'), 'utf8'))
  if (runtime.app !== identity.version || host.version !== identity.version || runtime.protocolVersion !== identity.protocolVersion || runtime.dsh !== identity.dsh || runtime.node !== identity.nodeRuntime || runtime.platform !== 'win32' || runtime.arch !== 'x64') throw new Error('Release identity does not match the packaged Host and runtime')
  const installer = `dsh-mygo-${identity.version}-windows-x64-setup.exe`
  const allowed = [installer, 'dsh-mygo-signing.cer', 'START_HERE.zh-CN.md', 'RELEASE_NOTES.md', 'SHA256SUMS.txt']
  const output = join(root, 'build/release', `v${identity.version}`)
  await mkdir(output, { recursive: true })
  if ((await readdir(output)).some(name => !allowed.includes(name))) throw new Error('Unexpected file in release output; use an empty release directory')
  await copyFile(join(app, `dsh-mygo Setup ${identity.version} amd64.exe`), join(output, installer))
  const certificate = await readFile(join(root, '.signing/dsh-mygo.cer'))
  await writeFile(join(output, 'dsh-mygo-signing.cer'), certificate)
  await copyFile(join(root, 'docs/getting-started.md'), join(output, 'START_HERE.zh-CN.md'))
  const fingerprint = digest(certificate).toUpperCase().match(/../g).join(':')
  await writeFile(join(output, 'RELEASE_NOTES.md'), `# dsh-mygo v${identity.version} · Windows 私有测试版

Windows x64 原生桌面客户端，安装包自带 Node ${identity.nodeRuntime} 与 DSH ${identity.dsh}，不需要另外安装开发环境。

## 开始使用

1. 下载并运行 \`${installer}\`，安装后从开始菜单打开 dsh-mygo。
2. 选择项目文件夹，点击「连接 DeepSeek」保存自己的 API key，再发送第一条消息。
3. 完整说明见附件 \`START_HERE.zh-CN.md\`；文件 SHA256 见 \`SHA256SUMS.txt\`。

## 本版包含

- 首次使用引导、项目文件夹选择与直接连接设置。
- 流式 Markdown、代码/源文复制、工具输出与只读 diff。
- 会话历史、草稿、模型/权限设置、审批/提问、图片与文件引用。
- 子代理任务、持久消息与结果查看，以及手动 Host 恢复。

## 测试版范围

- 仅在当前私有仓库分发，Windows x64；macOS、自动更新及后台跨会话交互留待后续版本。
- 应用可以离线启动；模型对话需要网络和有效 API key，费用由自己的 DeepSeek 账户承担。「已配置」不代表认证已验证。
- 凭据由 DSH 保存在本机，使用加密系统密钥库属于后续工作。
- 安装包使用自签名证书，Windows 可能显示 Unknown publisher/SmartScreen 提示。附件证书仅含公钥，无需为测试而导入系统信任。
- 验收使用开发机隔离数据目录；干净 Windows VM 与付费模型回合仍需测试者实际验证。

公开签名证书 SHA256 指纹：\`${fingerprint}\`。
`)
  const lines = []
  for (const name of allowed.filter(name => name !== 'SHA256SUMS.txt')) lines.push(`${digest(await readFile(join(output, name)))}  ${name}`)
  await writeFile(join(output, 'SHA256SUMS.txt'), `${lines.join('\n')}\n`)
  return output
}

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const verified = spawnSync(process.execPath, [join(root, 'scripts/verify-signatures.mjs')], { cwd: root, stdio: 'inherit' })
  if (verified.status !== 0) process.exit(verified.status ?? 1)
  console.log(`Release assets ready: ${await prepareRelease(root)}`)
}
