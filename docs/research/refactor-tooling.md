# 重构工具与持续护栏取舍

## 问题

阶段 5 需要决定是否为 Go/Node 协议词汇做代码生成、是否为 MyGo 主线程约束写自定义 analyzer，以及哪些度量/依赖/生成物检查值得进入 CI。结论首先是决策记录，不以“造出工具”为目标。

## 决策

### 不生成协议常量

Go 与 Node 的手写词汇已有双端 fixture（`tests/fixtures/protocol/`）与 conformance 测试。当前漂移由 fixture 直接暴露，频率低且新增/变更词汇仍需同时修改 handler、校验和测试；生成器还要定义 schema 覆盖、生成目录、导入路径与 Node ESM 消费方式，维护成本大于收益。保留 fixture/快照与双端测试作为护栏。若 fixture 连续多批出现同一种词汇漂移、且生成范围可稳定界定，再重新评估。

### 不写自定义主线程 analyzer

现有 `go tool mygo vet .` 已检查 UI 存储值/goroutine 捕获；阶段 2–4 的帧作用域问题由组件生命周期测试、`internal/view/build_test.go`（禁止 `Context.Memo`）和依赖方向测试覆盖。当前没有足够明确的真实线程问题需要跨包数据流分析，也没有可接受的误报率基线。保留普通 Go 类型、vet 与测试；若出现 MyGo vet 无法表达且能给出正/反例的线程问题，再做小范围 analyzer 或可验证的线程断言。

### AST 度量

`go run ./cmd/metrics -top 10` 只统计生产 Go 文件，跳过测试、生成文件、vendor、runtime/build 与隐藏目录；报告函数/方法/struct/字段总数及最大函数/struct。它是一个可复跑的观察工具，不是失败阈值，不把声明式 UI 长度当作缺陷。

2026-10-26 基线：131 个文件、363 个函数、452 个方法、211 个 struct、1161 个字段；最大函数为 `cmd/spike` 的 `run`（134 行）、`App.startHost`（133）、`App.hostReady`（113）；最大 struct 为 `appTokens`（32 字段）、`App`（31）。后续批次用同一命令比较趋势，差异需结合所有权/契约解释。

### 依赖与生成物检查

- `internal/view/imports_test.go` 禁止 View import app/channel/host/runtimes。
- `internal/projection/imports_test.go` 禁止投影 import app/view/channel/host/runtimes。
- `node scripts/check-repository.mjs` 拒绝跟踪 build/.signing/node_modules、runtime 的 Node/host 产物、manifest、tgz 与私钥/证书文件，并核对 `.gitignore` 入口。
- Windows CI 运行 metrics 与 repository check；Linux CI 继续 Go 构建/测试。

### CI 范围

race 与真实 mock Host job 暂不加入托管 CI：race 需要明确的 CGO/图形工具链，真实 Host 需要精确 DSH source/runtime、隔离 DSH_HOME 与 mock provider 配置；当前 runner 无法同时保证这些条件。它们继续在本地按 [测试策略](../subsystem/testing.md) 执行并记录证据。托管 CI 保留 Windows/Linux 构建、Go/Node/脚本测试、vet 与 MyGo vet。

## 重新评估触发条件

- fixture 后仍反复出现双端词汇漂移，且生成范围/维护成本可界定。
- 出现现有 vet/测试无法覆盖、且有明确正反例的主线程违规。
- 托管 runner 能提供稳定的 CGO/DSH 隔离环境，且 mock 链路无付费模型/用户配置依赖。
