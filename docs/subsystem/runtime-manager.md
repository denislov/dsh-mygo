# 运行时管理

运行时管理负责把“应用”和“DSH 后端”拆成独立发布单元：应用安装器只含本体；Node、DSH、私有 `mygo-host` 入口与配套 `@dsh-mygo/host` 作为一个 runtime bundle 分发，也允许使用本机已有的精确版本 DSH。

## 1. 存储与条目

运行时选择是**设备级**的，不按 `DSH_HOME` 或 profile 分区。存储位于 `mygo.App.Path(PathCache)/runtimes`（Windows `%LocalAppData%`），避免数百 MB 进入漫游目录。

```text
<runtime-home>/
├─ state.json
├─ downloads/            # 临时 .part，成功后删除
├─ .staging-*/           # 解压中的目录，启动时清理
├─ .trash-*/             # 待删除的 managed 目录
└─ dsh-<dsh>-node-<node>-host-<host>-<platform>-<arch>/
```

`state.json` v1：

```json
{
  "version": 1,
  "selected": "dsh-0.2.1-rc.2-node-24.14.0-host-0.1.1-win32-x64",
  "runtimes": [
    {
      "id": "dsh-0.2.1-rc.2-node-24.14.0-host-0.1.1-win32-x64",
      "kind": "managed",
      "dir": "C:\\Users\\me\\AppData\\Local\\dsh-mygo\\Cache\\runtimes\\dsh-...",
      "dsh": "0.2.1-rc.2",
      "node": "24.14.0",
      "hostVersion": "0.1.1",
      "installedAt": "2026-10-25T00:00:00Z"
    }
  ]
}
```

状态文件严格解析、限制 64 KiB、损坏时备份为 `state.json.corrupt-<nanos>` 并以空选择继续。写入使用同目录临时文件 + 原子 rename。

条目种类：

| kind | 含义 | 启动命令 | profile Host bundle |
| --- | --- | --- | --- |
| `managed` | 从 `.tgz` 安装的完整 bundle | `<dir>/node/node.exe <dir>/mygo-host.mjs <profile>` | `<dir>/host` |
| `external` | 用户已安装的精确版本 DSH | `dsh` 或经 `cmd.exe /d /s /c call` 的 `.cmd/.bat` | 应用自带 `host/` |
| `legacy` | 旧安装残留的 `resources/runtime` | 同 managed | 旧目录的 `host/` |
| `direct` | `-runtime-dir` 指定的已解压目录（开发/smoke） | 同 managed | 该目录 `host/` |

`legacy` 只在通过 manifest、DSH 包、Host bundle、平台与协议校验后列为引导候选，**不自动选中或启动**。`external` 不复制文件；只保存命令与 Node 路径。

## 2. Manifest 与 catalog

`runtime/manifest.json` 是 runtime 的身份：

```json
{
  "schemaVersion": 1,
  "kind": "dsh-runtime",
  "id": "dsh-0.2.1-rc.2-node-24.14.0-host-0.1.1-win32-x64",
  "dsh": "0.2.1-rc.2",
  "node": "24.14.0",
  "platform": "win32",
  "arch": "x64",
  "protocolVersion": 1,
  "hostVersion": "0.1.1",
  "appMin": "0.1.1",
  "appMaxExclusive": "0.2.0",
  "entry": "mygo-host.mjs",
  "hostBundle": "host"
}
```

要求：

- `id` 必须等于 `dsh-<dsh>-node-<node>-host-<hostVersion>-<platform>-<arch>`。
- `platform`/`arch` 使用 manifest 拼写（`win32`/`x64`），映射固定，不接受未知值。
- `protocolVersion` 与当前应用精确匹配。
- `appMin <= app < appMaxExclusive`（仅数字三段）。
- Host bundle 的 `name`、`version`、`engines.dsh`、`dshMygo.protocolVersion` 与 `appCompatibility` 必须与 manifest 一致。
- 入口、`node/` 与 `node_modules/@deepseek-ai/dsh/package.json` 必须存在，DSH 版本必须等于 `manifest.dsh`。

`runtime-catalog.json` 列出某个 GitHub Release 中可下载的 bundle：`id`、`dsh`、`node`、`platform`、`arch`、`protocolVersion`、`hostVersion`、`appMin`、`appMaxExclusive`、`asset`、`sha256`、`size`。解析时只保留当前平台/架构、协议与 app 范围兼容的条目。

## 3. 安装

`InstallArchive` 只做文件操作，**安装阶段不执行归档内任何程序**：

1. 限制归档 ≤1 GiB；读第一项，必须是 `manifest.json`，先校验身份与 app 范围。
2. 同卷创建 `.staging-*`，逐项校验并解压。
3. 校验 manifest、Node、DSH 包、Host bundle、入口后，原子 rename 为 `<id>`。
4. 写入 `state.json`；选择保持不变。

拒绝绝对路径、`..`、反斜杠、盘符/UNC、`:`（ADS）、NUL、符号链接/硬链接/设备/FIFO/稀疏项、Windows 保留名/尾随点或空格、大小写不敏感重复路径。上限：解压 ≤4 GiB、≤25 万条目、单文件 ≤512 MiB、路径 ≤1024 字节。

本地导入与下载安装走同一路径。

## 4. 下载

- 通过 GitHub Releases API 读取最近 20 个 release，取第一个含 `runtime-catalog.json` 的非 draft release（允许 prerelease）。
- 只信任 catalog 中的 asset 名称，下载 URL 由“该 release + asset”构造；后续若加镜像，必须显式加 host allowlist。
- 流式写入 `downloads/*.part`，边写边算 SHA-256；校验 catalog 的 `size`、`sha256` 与资产 `digest`（如有）。失败或取消删除临时文件。
- 使用系统代理；请求只带 `User-Agent: dsh-mygo/<version>`，不带 cookie/Authorization。
- catalog 在会话内缓存 15 分钟；403/429 显示限流说明，不伪造旧数据。

## 5. Existing DSH

`ProbeExternal` 依次：

1. 用 `exec.LookPath("dsh")`（或用户选择的路径）定位命令；`.cmd/.bat` 通过 `cmd.exe /d /s /c call <path> --version` 读取版本。
2. 用 `exec.LookPath("node")` 定位 Node，要求 24+。
3. 要求 DSH 版本与 `versions.json` 精确相等；否则返回 incompatible 与原因，不静默使用。

版本管理器（fnm/volta/nvm）可能把命令暴露在随 shell 重建的 shim 目录。持久化的 external 条目在命令或 Node 丢失时，会重新探测 PATH；只有 DSH 版本仍一致才更新条目，否则回到引导。启动失败必须可见，不回退到“PATH 上碰巧存在的 dsh”。

## 6. 启动解析

每次启动/重启都动态解析：

1. 显式开发入口优先：`-host-command`、`-dsh-dir`、`-runtime-dir`、SDK 回归。
2. 否则读取设备级选择并做完整校验。
3. 无有效选择 → `PhaseAwaitingRuntime`，渲染全窗口引导，不启动 Host。
4. 有效选择 → 构建命令、环境（`DSH_HOME`、PATH 前置选中 Node）与 profile seed 目录，再启动 Host。

启动时用 `initialize` 校验 Host 返回的 Host bundle 版本、DSH 版本与 app 兼容范围；身份不匹配视为该 runtime 不可用。

## 7. 切换

切换是事务式的：

```text
Idle/Failed
  → 停止旧 Host（invalidate generation）
  → 用候选 runtime 准备 profile Host bundle
  → 启动候选 Host + initialize + 恢复当前会话
  → 成功：持久化选中项，刷新 UI
  → 失败：停止候选，恢复旧 profile，重启旧 Host
          旧 Host 成功：设置显示“已恢复之前的运行时”
          旧 Host 失败：恢复对话框提供 Change runtime
```

- 只有 Host idle、无审批/提问、无会话/设置写入时允许切换；首次启动失败回引导。
- 候选恢复当前会话严格失败，不允许静默新建会话。
- 安装/下载可以在运行时进行，激活不能在会话忙时进行。
- 当前选中 runtime 不能移除；未选中 managed 条目移除时删除文件，external/legacy 只遗忘条目。

## 8. UI 契约

- 首启引导：已安装 DSH（精确版本）与 runtime bundle 两种来源；显示检测/下载/解压进度、错误、取消；最小窗口 560×420 可达。
- 设置中心 Runtime 分区始终可用，不访问 Host；显示当前 runtime 的类型、DSH、Node、Host 版本与路径，已安装条目可 Use/Remove，添加区可检测 DSH、从文件安装、检查更新。
- 下载/安装/切换期间禁止关闭设置，提供进度与取消（切换显示等待状态）。
- 恢复对话框提供 Change runtime…，无需重启应用。
- UI 只保存显示状态与回调，不保存 `ui.Element`/`ui.Context`；后台 I/O 结果通过 `Window.Update` 发布。

## 9. 安全与日志

- runtime 是可执行第三方代码；只从 HTTPS GitHub 资产或用户选择的本地文件安装，显示“以当前用户权限运行”的说明。
- 校验 SHA-256/size/digest；不接受 catalog 注入的任意 URL。
- 日志只记录 runtime id、DSH、Host 版本、来源与字节数；不记录下载 URL 查询串、catalog 原文、凭据或会话内容。
- 运行时只写用户数据目录，不写应用安装目录。

## 10. 验证

- `go test ./internal/runtimes`：manifest/catalog、状态损坏、归档路径/链接/上限、原子安装、SHA/取消、external `.cmd` 与 shim 修复、legacy。
- `go test ./internal/app ./internal/view`：无运行时进入引导、安装后启动、事务式切换成功/回滚、设置门禁与最小窗口。
- `node --test scripts/*.test.mjs`：确定性 bundle、catalog、release 资产与校验和。
- `node scripts/smoke-packaged.mjs`、`node scripts/smoke-installer.mjs`：安装器不含 runtime、首启引导、显式 runtime 启动、卸载保留 `$DSH_HOME`。
- 真实窗口：外部 DSH → managed 切换，会话与草稿保留；失败候选回滚。
