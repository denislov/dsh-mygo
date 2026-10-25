# 实施计划：思考显示、活动折叠与视觉统一

## 阶段 1：持久思考投影与活动组（提交/历史）

**目标**: 已提交与本机历史中的 reasoning 成为一等行，并折叠进默认收起的活动组。
**成功标准**: 历史回放显示活动组；展开看到思考原文与工具卡片；reasoning 不混入 assistant 正文；预算、截断、seq 去重、历史边界保持。
**测试**: projection/markdown/history 单测；view activity Tester；fake host 历史回放；`go test ./internal/projection ./internal/view ./internal/app`。
**状态**: 已完成

## 阶段 2：实时流式思考与恢复 baseline

**目标**: 流式 reasoning 实时进入同一活动组；resync 后已流出的思考可续接。
**成功标准**: `Thinking…`/`Working…` 状态正确；preview→commit 组不闪断；capability 门控下 baseline 携带 reasoning，旧组合行为不变。
**测试**: stream/app 单测与集成；Node baseline/history 协商测试；fake host `--reasoning`；真实 Host。
**状态**: 已完成

## 阶段 3：对话区视觉统一

**目标**: 消息排版、活动组、思考文本、工具卡片、Markdown、空状态达到 Claude Desktop 风格。
**成功标准**: 明暗一致，宽/窄/最小窗口无回归，颜色只走 tokens。
**测试**: view Tester、markdown/layout/style 测试、Windows 原生窗口。
**状态**: 进行中

## 阶段 4：全界面外壳视觉统一

**目标**: 标题栏、侧栏、composer、details、设置与弹窗纳入同一视觉体系。
**成功标准**: 统一尺寸/圆角/间距/字号与状态；对比度测试通过；功能与可达性不变。
**测试**: 外壳相关 Tester、对比度用例、最小窗口与明暗截图。
**状态**: 未开始

## 阶段 5：文档、回归与验收

**目标**: 契约文档与实现一致，完整验证闭环，删除本计划。
**成功标准**: 指定文档更新；全量测试、真实 Host、Windows 原生窗口通过。
**测试**: `CGO_ENABLED=0 go test ./...`、race、MyGo vet、go vet、Node 测试、真实 Host、原生窗口。
**状态**: 未开始
