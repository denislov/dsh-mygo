# 打包与更新

## 职责

把 Go UI、Node Host、DSH runtime 与 profile 组合成一个可安装、可签名、可更新的桌面应用，并保证：

- 首启离线可用。
- UI、Host、bundle 版本一致。
- 更新不破坏 `$DSH_HOME` 中的会话、设置、凭据与第三方插件。
- 签名、公证、安装器与更新链路可验证。

## 应用布局

```text
dsh-mygo.app / dsh-mygo/            # MyGo 产物
├─ dsh-mygo(.exe)                   # Go 二进制
└─ resources/                       # mygo.App.Path(mygo.PathResources)
   ├─ host/                         # @dsh-mygo/host bundle，也是 profile seed 源
   ├─ runtime/
   │  ├─ mygo-host.mjs              # 私有入口：调用 runProfile
   │  ├─ package.json               # 精确 pin @deepseek-ai/dsh
   │  ├─ package-lock.json
   │  ├─ node/                      # 独立 Node（生成，不提交）
   │  ├─ node_modules/              # DSH 生产依赖闭包（生成，不提交）
   │  ├─ manifest.json              # 应用/DSH/Node/协议版本（生成）
   │  └─ primary-runtime/           # 可选：Python/Office/pnpm
   └─ icon.png                      # 窗口与安装器素材
```

`resources/` 的布局由构建脚本生成；运行时只通过 `mygo.App.Path(mygo.PathResources)` 读取，不依赖工作目录。`resources/host`、`resources/runtime/mygo-host.mjs`、Node 与 DSH 依赖闭包是发布包的一部分。`runtime/node/`、`runtime/node_modules/` 与 `runtime/manifest.json` 由构建生成，不进入版本库。

`resources/runtime/go.mod` 是一个空的嵌套模块，只用于让 `go build ./...` 与 `go test ./...` 跳过 `runtime/node_modules`；它不参与 Node 依赖解析。

## 构建

0. `mygo.json` 的 `identifier` 是正式 app id；发布后不要更改，否则更新与系统集成会分裂。

1. `node scripts/build-runtime.mjs`：
   - 下载 `versions.json` 固定的 Node，并用 `SHASUMS256.txt` 校验 SHA256。
   - 在 `resources/runtime` 执行 `npm ci --omit=dev`，安装精确 pin 的 `@deepseek-ai/dsh`。
   - `scripts/runtime-assets.mjs` 将固定 pi-ai 依赖的公开 `.manifest.json` 复制为 `models-manifest.json` 并调整生成目录中的引用，保留 JSON 字节；仅作用于构建产物，不改 DSH core。MyGo 的资源复制过滤点号开头文件，必需运行时数据须使用可分发名称；依赖布局变化时构建明确失败。
   - 写入 `runtime/manifest.json`：应用版本、DSH 版本、Node 版本、平台、架构。
2. `mygo.json` 的 `buildCommand` 在 `go tool mygo build` 前自动运行上述脚本。
3. `go tool mygo build`：编译 Go 二进制、复制 `resources/`，生成 Windows 安装器；macOS 随后生成 `.app`/`.dmg`。
4. profile seed 不预生成目录：首启由 Host 启动前从 `resources/host` 复制到 `$DSH_HOME/profiles/mygo`；补丁升级只刷新应用自有 Host 包并备份旧包，保留 profile 配置与数据，见 [宿主监督](host-supervisor.md#首启-profile)。
5. 签名/公证（见下）。
6. 生成更新清单与 delta（如启用更新）。

构建产物必须可重现：Node 版本、依赖 lock、DSH 版本、bundle 版本都固定。

## 版本

| 版本 | 来源 | 规则 |
| --- | --- | --- |
| 应用标识/名称/版本 | `versions.json`（镜像到 `mygo.json`） | 发布单元的主版本；发布后 identifier 不再更改 |
| DSH 版本 | `versions.json`（镜像到 host `engines.dsh` 与 peer 依赖） | 与 bundle 的 peer 依赖对齐 |
| Node 运行时 | `versions.json` 的 `nodeRuntime`（写入 `runtime/manifest.json`） | 下载时校验 SHA256 |
| bundle 版本 | `@dsh-mygo/host` | 与 app 同版本或显式兼容 |
| 协议版本 | `versions.json` / host `dshMygo.protocolVersion` | UI 与 Host 的额外保险 |
| profile 格式 | DSH profile manifest | 由 DSH 负责迁移 |

`versions.json` 是发布身份的唯一来源；`mygo.json`、`resources/host/package.json` 与 `internal/version` 的 Go 常量由 `go test ./internal/version` 校验一致。

UI、Host、bundle 作为一个发布单元验证；不允许只升级其中一个。

## 首启

安装版未显式指定 `-cwd` 时，不将开始菜单或安装目录的进程 cwd 当成项目，初始会话使用当前用户主目录；空会话要求在原生引导中选择或确认工作文件夹。显式 cwd 保持优先，已有会话恢复自己的持久目录。工作文件夹选择通过创建新会话实现，不写回旧历史，详见 [UI 壳层](ui-shell.md#首次使用与工作文件夹)。

1. 解析 `DSH_HOME`（默认 `~/.dsh`）。
2. 若 `profiles/mygo` 不存在，Host 启动前由 `host.EnsureProfile` 从 `resources/host` 复制创建；不覆盖已有 profile。失败时窗口显示 `Host error`，见 [宿主监督](host-supervisor.md#首启-profile)。
3. 校验 profile 的 `dsh.profile.bundles` 包含 base 与 mygo bundle；缺失时提示修复命令。
4. 校验 bundle 版本与 app 版本；不匹配时拒绝启动并给出修复建议。
5. 启动 Host；首启不运行 pnpm。
6. 第三方插件由用户通过 `dsh plugin --profile mygo add` 或 UI 插件管理页安装；安装时使用 bundled pnpm 与 profile 锁。

## 打包内容

### 必须

- Go 二进制。
- 私有 `mygo-host`（或 `dsh` + profile runner）。
- `@deepseek-ai/dsh` 及生产依赖。
- `@your-scope/dsh-mygo-app`。
- 独立 Node。
- profile seed。
- 图标、托盘素材、许可文件。

### 可选

- pnpm（第三方插件安装）。
- Python/Office runtime（Office 技能）。
- `dsh` CLI 包装脚本。
- 额外 skill/asset。

可选内容按产品能力启用；不要为了“完整”无条件携带。

## 签名与公证

### Windows（首发）

使用本地自签名证书，不购买公开 CA 证书。签名身份由 `scripts/self-sign.mjs` 生成到 `.signing/`：

- `.signing/dsh-mygo.pfx` 保存私钥，`dsh-mygo.cer` 是公钥证书，`password.txt` 与 `subject.txt` 是本地构建状态；`.signing/` 不进入版本库。
- 默认 3072 位 RSA、3 年有效期，`keyUsage=digitalSignature`、`extendedKeyUsage=codeSigning`。
- `mygo.json` 的 `windows.signCommand` 调用 `scripts/sign-windows.mjs`，签名 Go 二进制、安装器、卸载器，以及 `resources/` 中未签名的 PE 文件；已有发布者签名的文件（如 Node 的 `node.exe`）跳过。
- 首次 `go tool mygo build` 自动生成证书；`DSH_MYGO_SIGN=0` 跳过签名，构建日志仍显示 MyGo 的 `Signing`/`Signed` 步骤文案，但产物未签名。
- 时间戳默认 `http://timestamp.digicert.com`；服务器不可达时退回无时间戳签名。
- 本机信任：`node scripts/self-sign.mjs --trust` 写入 CurrentUser 的 Root 与 TrustedPublisher；`--remove-trust` 移除。`node scripts/verify-signatures.mjs` 校验签名，`--require-trusted` 要求证书链受信。
- 自签名证书不建立 SmartScreen 信誉：Windows 显示 Unknown publisher，安装器可能被拦截。发布时附 `.cer`、SHA-256 指纹与产物校验和，不承诺首次安装无提示；开源项目后续可申请 SignPath Foundation 的免费 OSS 签名。
- 安装器 per-user、无需管理员；卸载保留 `$DSH_HOME`。

### macOS（随后）

- Developer ID Application 证书；hardened runtime。
- 对 app bundle 内所有可执行文件/框架签名；Node、原生 `.node`、helper 都要签。
- 公证（notarization）与 stapling。
- entitlements：JIT（Node/V8）、麦克风（如启用语音）、网络客户端。
- 更新包同样签名/公证。

### Linux（不首发）

- Debian 包/安装脚本；WebKitGTK 等系统依赖不适用（原生 UI 不需要）。
- 若使用系统 Node，需声明依赖；建议仍携带独立 Node。

## 更新

### 私有测试版分发

- Windows x64 测试版通过现有私有 GitHub 仓库的 prerelease 分发，保留源码与资产访问范围；tag 为 `v<versions.json.version>`，显式指定已经提交并通过检查的目标 commit，不使用浮动 HEAD 代替发布身份。
- `node scripts/prepare-release.mjs` 先校验安装器/应用签名，再检查发布版本、Host 与 runtime manifest 的平台/Node/DSH/协议身份；仅复制具名安装器、公开 `.cer` 与首次使用指南，并生成发布说明和 SHA256SUMS。输出为 `build/release/v<version>`，存在未知文件时拒绝生成，不扫描或复制 `.env`、PFX、密码或开发目录。
- 上传资产为稳定名称的 `dsh-mygo-<version>-windows-x64-setup.exe`、`dsh-mygo-signing.cer`、`START_HERE.zh-CN.md`、`SHA256SUMS.txt` 与 `RELEASE_NOTES.md`。公开证书 SHA256 指纹写入说明；证书分发不自动修改测试者信任存储。上传后核对文件名、大小与 GitHub 返回的 SHA256 digest。
- 发布使用 `gh release create` 的 `--draft --prerelease --target <commit> --notes-file <file>` 准备，资产核对后再取消 draft。不改变仓库可见性、不覆盖已有 tag 或 release。测试版发布不表示干净 VM、macOS、自动更新或付费模型验收通过，这些状态仍在路线图记录。
- 首版不携带自动更新器，用户退出应用后运行新版本安装器覆盖安装；数据迁移与保留规则遵循下述更新契约。

使用 `mygo.Updater`：

- 更新源：GitHub Releases、S3 兼容存储或自有 HTTPS。
- 清单含版本、平台、架构、URL、大小、签名；应用内置公钥。
- 支持 delta 更新。
- 更新窗口/确认由 dsh-mygo 自己实现（`ui.Modal` 或原生窗口）。

### 更新流程

1. 检查更新（可手动/定时）。
2. 下载并校验签名。
3. 等待 Host 空闲：没有运行中的回合、没有待处理审批/提问、没有正在停止的 job。
4. 停止 Host（`shutdown`，超时升级）。
5. 安装更新；应用退出/重启。
6. 新版本启动：校验 runtime manifest、profile、bundle 版本；恢复会话。

### 任务保护

- 有运行中回合时，更新确认必须说明会中断任务。
- 用户拒绝时保留已下载包与“已就绪”状态。
- Host 无法确认空闲时，不自动安装。
- 更新失败时保留旧版本与数据；显示可操作错误。

### 迁移

- 从 Electron Desktop 或 npm CLI 迁移：共享 `$DSH_HOME` 的会话、设置、凭据；不共享可执行包与锁文件。
- 首个 MyGo 版本无法通过 electron-updater 自动升级；需要一次手动安装或由旧版本下载并启动新安装器。
- 第三方插件按 DSH 的 profile 规则重新解析；原生模块 ABI 变化时提示修复。
- 旧 Desktop profile 不由 dsh-mygo 启动；使用独立的 `mygo` profile。

## CLI 与系统集成

- 可选安装 `dsh-mygo` 命令；启动器可复用 `bin/dsh-tui.js` 的自举思路。
- `dsh` CLI 仍可用于管理 profile：`dsh plugin --profile mygo ...`。
- 深链 `dsh-mygo://session/<id>`：注册 scheme，解析后打开对应会话。
- 单实例：第二次启动把参数交给第一个实例。
- 托盘、通知、文件关联按平台实现；通知遵循系统权限。

## 日志与崩溃

发布构建写入：

| 内容 | 位置 |
| --- | --- |
| Go 日志 | `PathLogs/dsh-mygo.log`（追加） |
| Host stderr 尾部 | 内存环形缓冲；失败时写入崩溃报告 |
| 崩溃报告 | `PathLogs/crash-<UTC>-<source>.log` |
| 更新日志 | `PathLogs/update-*.log` |

报告包含版本、平台、Host 进程 id、最后错误、当前会话 id 与 stderr 尾部；写入前把 API key、Bearer token、password/secret 形状的值替换为 `[REDACTED]`，不写环境变量、cookie 或附件内容。崩溃报告与 Go 应用日志同目录；恢复对话框的 **View logs** 打开该目录。

## 测试

- 运行时资产必须在复制后的目录校验：确认 pi-ai 引用的清单为可分发文件，使用 bundled Node 实际导入 pi-ai provider；源依赖目录的成功不能代替复制后结果。签名校验与 smoke 按 `versions.json` 精确选择本版安装器，目录中保留旧安装器时也不能误验旧版。
- 用户已有安装时，安装/卸载 smoke 保持拒绝覆盖的保护；可对候选安装器做只读解包资产校验，并使用隔离 profile 验证原生连续启动与旧 Host 包升级。用户安装目录的修复或升级保留模型、凭据与会话，验证时不发送付费 prompt。

- 打包 smoke：`node scripts/smoke-packaged.mjs` 检查 `build/windows-amd64` 的 exe、安装器、host、Node、DSH 闭包与 `runtime/manifest.json` 版本；调用 `scripts/verify-signatures.mjs` 校验 exe 与安装器签名；用空 `PATH`、临时 `DSH_HOME` 与临时日志目录启动 exe，等待 profile、session 与应用日志；随后对打包资源跑 composition + restart 真实组合测试。
- 安装器 smoke：`node scripts/smoke-installer.mjs` 静默安装到 `.tmp`，校验安装布局、Start Menu 快捷方式与 HKCU 卸载键、安装后 exe 与 `Uninstall.exe` 签名；启动安装后的应用，等待 profile、session 与日志；再静默卸载，确认安装目录、快捷方式和卸载键被移除，且 `$DSH_HOME` 与会话历史保留。检测到已有 dsh-mygo 安装时拒绝运行；发布验收使用干净 Windows VM。`--keep` 保留临时目录。
- 签名校验：Windows `signtool verify`（`node scripts/verify-signatures.mjs`，可用 `--require-trusted` 要求证书链受信）；安装器 smoke 额外校验 `Uninstall.exe`；macOS `codesign --verify` 与公证。
- 更新 smoke：旧版本 → 新版本，保留会话/设置；delta 路径。
- 断网/磁盘满/权限错误。
- 任务运行中更新被拒绝/确认后中断。
- 卸载保留 `$DSH_HOME`。

## 未决问题

- 更新源（GitHub Releases / 自有源）与是否首版支持 delta。
- 是否携带完整 `primary-runtime`（Python/Office）。
- Windows 安装器格式（NSIS / MSI）与 per-user 策略的最终选择。
- macOS 发布窗口与公证凭证归属。

## 相关文档

- [宿主监督](host-supervisor.md)
- [profile 与 bundle](profile-bundle.md)
- [进度与路线](../roadmap.md)
- [架构](../architecture.md)
- [研究：MyGo 原生 UI](../research/mygo-native-ui.md)
