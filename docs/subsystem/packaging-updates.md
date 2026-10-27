# 打包与更新

## 职责

应用安装器只含本体；Node + DSH + 私有入口 + 配套 Host bundle 作为独立 runtime 资产发布。打包需保证：

- 应用安装器可独立安装、启动并进入运行时引导。
- 离线环境可通过本地 `.tgz` 或已安装 DSH 完成首启；不要求联网下载。
- 应用与 runtime 作为独立发布单元，通过 manifest 的协议与 app 兼容范围组合。
- 更新应用或切换 runtime 不破坏 `$DSH_HOME` 中的会话、设置、凭据与第三方插件。
- 签名、安装器、运行时下载与校验链路可验证。

## 应用布局

```text
dsh-mygo.app / dsh-mygo/            # MyGo 产物
├─ dsh-mygo(.exe)                   # Go 二进制
└─ resources/                       # mygo.App.Path(mygo.PathResources)
   ├─ host/                         # @dsh-mygo/host bundle（external DSH 与 profile seed）
   └─ icon.png                      # 窗口与安装器素材
```

安装器不包含 `runtime/`、Node 或 DSH。`resources/` 的布局由 MyGo 构建复制；运行时只通过 `mygo.App.Path(mygo.PathResources)` 读取，不依赖工作目录。

## Runtime 资产布局

```text
runtime/                            # 构建源，独立于应用安装目录
├─ package.json                     # 精确 pin @deepseek-ai/dsh
├─ package-lock.json
├─ mygo-host.mjs                    # 私有入口：调用 runProfile
├─ go.mod                           # 空嵌套模块，隔离 node_modules
├─ node/                            # 独立 Node（生成，不提交）
├─ node_modules/                    # DSH 生产依赖闭包（生成，不提交）
├─ host/                            # 构建时从 resources/host 复制（生成，不提交）
└─ manifest.json                    # runtime 身份（生成，不提交）
```

`.tgz` 根目录即上述内容。`runtime/node/`、`runtime/node_modules/`、`runtime/host/` 与 `runtime/manifest.json` 由构建生成，不进入版本库；`runtime/go.mod` 只用于让 `go build ./...` / `go test ./...` 跳过 `node_modules`。资产命名与 manifest 字段见 [运行时管理](runtime-manager.md)。

## 构建

0. `mygo.json` 的 `identifier` 是正式 app id；发布后不要更改，否则更新与系统集成会分裂。

1. `node scripts/build-runtime.mjs`：
   - 只支持 Windows x64（macOS 随后）。
   - 下载 `versions.json` 固定的 Node，并用 `SHASUMS256.txt` 校验 SHA256。
   - 在 `runtime/` 执行 `npm ci --omit=dev`，安装精确 pin 的 `@deepseek-ai/dsh`。
   - 将 `resources/host` 复制为 `runtime/host`（产物，不提交），校验包版本、`engines.dsh`、协议与 app 范围。
   - 写入 `runtime/manifest.json`；把 `runtime/` 打成确定性 `.tgz`：`manifest.json` 第一项、路径排序、固定 mtime/UID/GID/mode、包含隐藏文件、排除 `go.mod`。
   - 生成 `runtime-catalog.json` 与 SHA256/size；输出 `build/runtime/`。
2. `go tool mygo build`：编译 Go 二进制、复制 `resources/`（仅 `host/` 与图标），生成 Windows 安装器；不再运行 runtime 构建，也不把 `runtime/` 打进应用。
3. profile seed 不预生成目录：首启由 Host 启动前从所选 runtime 的 `host/`（managed/legacy/direct）或应用 `resources/host`（external）复制到 `$DSH_HOME/profiles/mygo`；内容指纹变化时刷新 Host 包并备份旧包（同版本开发构建同样生效），保留 profile 配置与数据，见 [宿主监督](host-supervisor.md#首启-profile)。
4. 签名/公证（见下）。
5. 生成应用发布资产与 runtime 发布资产，分别校验。

构建产物必须可重现：Node 版本、依赖 lock、DSH 版本、bundle 版本与归档元数据都固定。

## 版本

| 版本 | 来源 | 规则 |
| --- | --- | --- |
| 应用标识/名称/版本 | `versions.json`（镜像到 `mygo.json`） | 应用发布单元的主版本；发布后 identifier 不再更改 |
| DSH 版本 | `versions.json`（镜像到 host `engines.dsh`、runtime manifest 与 peer 依赖） | 同一 runtime 内精确一致；existing DSH 必须精确匹配 |
| Node 运行时 | `versions.json` 的 `nodeRuntime`（写入 runtime manifest） | 下载与安装时校验；Node 24+ |
| Host bundle 版本 | `resources/host/package.json` / runtime manifest `hostVersion` | 应用与 runtime 携带的副本必须各自与 manifest 一致 |
| App 兼容范围 | `versions.json.appCompatibility` / host `dshMygo.appCompatibility` / runtime manifest | `appMin <= app < appMaxExclusive`，数字三段 |
| 协议版本 | `versions.json.protocolVersion` / host `dshMygo.protocolVersion` / runtime manifest | 精确匹配；破坏性变更提升 |
| profile 格式 | DSH profile manifest | 由 DSH 负责迁移 |

`versions.json` 是发布身份的唯一来源；`mygo.json`、`resources/host/package.json`、runtime manifest 与 `internal/version` 的 Go 常量由测试校验一致。

应用与 runtime 独立升级：应用安装器不携带 runtime；runtime 通过 manifest 声明协议、Host bundle 与 app 范围。发布验收必须覆盖当前支持的组合，而不是只验证一个锁步版本。

## 首启

安装版未显式指定 `-cwd` 时，不将开始菜单或安装目录的进程 cwd 当成项目，初始会话使用当前用户主目录；空会话要求在原生引导中选择或确认工作文件夹。显式 cwd 保持优先，已有会话恢复自己的持久目录。工作文件夹选择通过创建新会话实现，不写回旧历史，详见 [UI 壳层](ui-shell.md#首次使用与工作文件夹)。

1. 解析 `DSH_HOME`（默认 `~/.dsh`）。
2. 读取设备级 runtime store；没有有效选择时显示运行时引导，提供“使用已安装 DSH”和“安装 runtime bundle（下载 / 本地 `.tgz`）”。
3. 选择后校验 manifest/命令、Node 24+、DSH 版本、协议与 app 范围；成功后持久化。
4. 若 `profiles/mygo` 不存在，Host 启动前由 `host.EnsureProfile` 从所选 runtime 的 Host bundle 复制创建；不覆盖已有 profile。失败时窗口显示 `Host error`，见 [宿主监督](host-supervisor.md#首启-profile)。
5. 校验 profile 的 `dsh.profile.bundles` 包含 base 与 mygo bundle；缺失时提示修复命令。
6. 启动 Host；首启不运行 pnpm。
7. 第三方插件由用户通过 `dsh plugin --profile mygo add` 或 UI 插件管理页安装；安装时使用 bundled pnpm 与 profile 锁。

安装版未显式指定 `-cwd` 时，runtime 引导先于工作目录选择；Host 就绪后仍按上述工作文件夹规则进入空会话。`-host-command`、`-dsh-dir`、`-runtime-dir` 与 SDK 回归是开发/测试入口，不写设备级选择。

## 打包内容

### 应用安装器（必须）

- Go 二进制。
- `@dsh-mygo/host` bundle（external DSH 与 profile seed）。
- 图标、托盘素材、许可文件。

### 应用安装器（不得包含）

- `runtime/`、Node、`@deepseek-ai/dsh` 生产依赖闭包、私有 `mygo-host` 入口。
- 会话、凭据、用户 profile。

### Runtime `.tgz`（必须）

- 私有 `mygo-host` 入口。
- `@deepseek-ai/dsh` 及生产依赖。
- 独立 Node。
- 配套 `@dsh-mygo/host` bundle。
- `manifest.json`。

### Runtime `.tgz`（可选）

- pnpm（第三方插件安装）。
- Python/Office runtime（Office 技能）。
- 额外 skill/asset。

可选内容按产品能力启用；不要为了“完整”无条件携带。

## 签名与公证

### Windows（首发）

使用本地自签名证书，不购买公开 CA 证书。签名身份由 `scripts/self-sign.mjs` 生成到 `.signing/`：

- `.signing/dsh-mygo.pfx` 保存私钥，`dsh-mygo.cer` 是公钥证书，`password.txt` 与 `subject.txt` 是本地构建状态；`.signing/` 不进入版本库。
- 默认 3072 位 RSA、3 年有效期，`keyUsage=digitalSignature`、`extendedKeyUsage=codeSigning`。
- `mygo.json` 的 `windows.signCommand` 调用 `scripts/sign-windows.mjs`，签名 Go 二进制、安装器与卸载器；`resources/` 只有 Host bundle 与图标。runtime `.tgz` 不签名，使用 GitHub HTTPS + catalog SHA256（Node 自带的 `node.exe` 保留其上游签名）。
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

### 公开 GitHub Releases

- 仓库公开；应用与 runtime 使用独立 release。应用 tag 为 `v<versions.json.version>`；runtime 建议 tag 为 `runtime-v<dsh>-node<node>-<platform>-<arch>`。发布时显式指定已提交并通过检查的目标 commit，不使用浮动 HEAD。
- `node scripts/prepare-release.mjs` 校验安装器/应用签名、版本、Host bundle 身份，并拒绝包含 `runtime/` 的安装布局；仅复制具名安装器、公开 `.cer`、首次使用指南与发布说明，生成 SHA256SUMS。输出为 `build/release/v<version>`，存在未知文件时拒绝生成。
- `node scripts/prepare-runtime-release.mjs` 校验 runtime catalog、tgz SHA256/size、平台与 app 兼容范围，并重新检查归档布局；输出 `build/release/runtime-<id>`，包含 `.tgz`、`runtime-catalog.json`、`RUNTIME_NOTES.md` 与 `SHA256SUMS.txt`。
- 应用资产：`dsh-mygo-<version>-windows-x64-setup.exe`、`dsh-mygo-signing.cer`、`START_HERE.zh-CN.md`、`SHA256SUMS.txt`、`RELEASE_NOTES.md`。runtime 资产：catalog、`.tgz`、说明与校验和。上传后核对文件名、大小与 GitHub digest。
- 应用通过 GitHub Releases API 查找最新非 draft 且含 `runtime-catalog.json` 的 release；不扫描更旧的 release 来“凑”兼容版本。
- 首版不携带自动更新器；用户退出应用后运行新版本安装器覆盖安装。runtime 可在设置中下载/切换，数据保留规则遵循下述更新契约。

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
6. 新版本启动：校验应用身份、已选 runtime 的 manifest 与兼容范围、profile 与 Host bundle；恢复会话。应用更新不要求 runtime 同步更新，但兼容范围必须覆盖新应用版本。

运行时更新独立进行：设置中检查更新 → 下载/校验 → 安装到 store（不切换）→ 用户选择 Use → 事务式切换；失败保留旧 runtime。

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

- 运行时资产必须在解压后的目录校验：manifest 第一项、Node/DSH/Host bundle 身份、平台与 app 范围、确定性归档元数据；使用 bundled Node 实际导入 DSH provider。源依赖目录的成功不能代替解压后结果。
- 签名校验与 smoke 按 `versions.json` 精确选择本版安装器，目录中保留旧安装器时也不能误验旧版。
- 用户已有安装时，安装/卸载 smoke 保持拒绝覆盖的保护；使用隔离 profile 验证原生连续启动与旧 Host 包升级。用户安装目录的修复或升级保留模型、凭据与会话，验证时不发送付费 prompt。

- 打包 smoke：`node scripts/smoke-packaged.mjs` 检查 `build/windows-amd64` 的 exe、安装器、host 与图标，并断言安装布局不含 `runtime/`；校验独立 runtime（默认 `runtime/`，可用 `DSH_MYGO_RUNTIME_DIR` 覆盖）的 manifest 与版本。首次用空 runtime store 启动，断言不创建 profile/session、不启动 Host；再用 `-runtime-dir` 启动，等待 profile、session、日志与 host pid，并用同一 `DSH_HOME`/UI 状态再次启动确认恢复同一会话。
- 安装器 smoke：`node scripts/smoke-installer.mjs` 静默安装到 `.tmp`，校验安装布局（无 runtime）、Start Menu 快捷方式与 HKCU 卸载键、安装后 exe 与 `Uninstall.exe` 签名；首次启动验证运行时引导，再用独立 runtime 启动并等待 profile/session；卸载后确认安装目录、快捷方式和卸载键被移除，且 `$DSH_HOME` 与会话历史保留。检测到已有 dsh-mygo 安装时拒绝运行；发布验收使用干净 Windows VM。`--keep` 保留临时目录。
- 运行时发布：`node --test scripts/prepare-runtime-release.test.mjs` 校验 catalog/归档/兼容范围；`node scripts/prepare-runtime-release.mjs` 生成独立资产。
- 签名校验：Windows `signtool verify`（`node scripts/verify-signatures.mjs`，可用 `--require-trusted` 要求证书链受信）；安装器 smoke 额外校验 `Uninstall.exe`；macOS `codesign --verify` 与公证。
- 更新 smoke：旧版本 → 新版本，保留会话/设置；delta 路径。
- 断网/磁盘满/权限错误。
- 任务运行中更新被拒绝/确认后中断。
- 卸载保留 `$DSH_HOME`。

## 未决问题

- 应用自动更新器与 delta；首版仍为手动覆盖安装。
- runtime 增量/断点续传与镜像源；当前整包下载 + SHA256。
- 是否携带完整 `primary-runtime`（Python/Office）。
- 运行时 detached signature（当前为 HTTPS + catalog SHA256）。
- Windows 安装器格式（NSIS / MSI）与 per-user 策略的最终选择。
- macOS runtime 产物、公证与发布窗口。

## 相关文档

- [宿主监督](host-supervisor.md)
- [运行时管理](runtime-manager.md)
- [profile 与 bundle](profile-bundle.md)
- [进度与路线](../roadmap.md)
- [架构](../architecture.md)
- [研究：MyGo 原生 UI](../research/mygo-native-ui.md)
