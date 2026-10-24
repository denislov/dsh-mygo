# 实施计划：应用与 DSH 运行时拆分

## 阶段 1: 运行时 bundle 契约与构建管线

**目标**: 应用构建不再包含 Node/DSH；生成可单独分发的 `.tgz`、manifest 与 catalog。
**成功标准**: `runtime/` 移出 `resources/`；`node scripts/build-runtime.mjs` 生成确定性 tgz/catalog；Host 按 app 兼容范围握手；旧 dotfile 改写移除。
**测试**: `go test ./internal/version ./internal/channel ./internal/host`；`node --test resources/host/tests/*.test.mjs`；`node --test scripts/*.test.mjs`；实际构建并检查 tgz。
**状态**: 已完成

## 阶段 2: Runtime manager 与动态 Host 启动

**目标**: 应用可选择/安装/校验/切换运行时，启动流程动态解析 Host。
**成功标准**: `internal/runtimes` 完成 store/archive/download/external/legacy；`app` 动态 launch；无运行时进入 setup 状态；开发/fake/SDK 路径保持。
**测试**: Go 单元与集成测试、竞态测试、归档安全测试。
**状态**: 已完成

## 阶段 3: 首启运行时引导

**目标**: 无运行时启动显示原生引导，可选用已有 DSH 或安装 bundle。
**成功标准**: 独立全窗口 setup；两种来源、进度、错误、取消；560×420 可达；成功后启动 Host。
**测试**: `ui.NewTester`、app 状态测试、真实窗口截图。
**状态**: 未开始

## 阶段 4: 设置中心 Runtime 分区与切换/更新

**目标**: 设置中心支持查看、下载、更新、切换、移除运行时。
**成功标准**: Runtime 分区始终可用；事务式切换与失败回滚；恢复对话框可更换运行时；最小窗口可达。
**测试**: view/app 测试、真实 Host 切换与会话恢复、UIA 验收。
**状态**: 未开始

## 阶段 5: 发布、smoke、文档与验收

**目标**: 应用安装器与 runtime tgz 独立发布；文档与验收完整。
**成功标准**: 安装器仅本体；smoke 覆盖无 runtime 首启与 `-runtime-dir`；文档更新；真实窗口验收。
**测试**: 全量 Go/Node/脚本检查、打包与安装 smoke、真实窗口。
**状态**: 未开始
