# 通道协议（dsh-mygo channel）

## 职责

通道协议连接两件东西：

- **Go UI**：MyGo 进程，发起请求、消费事件、展示界面。
- **Host 插件**：`@dsh-mygo/host`，在 DSH 的 Cordis 树中调用 agent/session/approval/user-questions 服务。

协议只描述消息、顺序与错误，不描述 UI 布局，也不复制 DSH 的业务方法。

## 消息集与能力

- UI/Host 发布版本 `0.1.0`、`protocolVersion: 1`、DSH `0.2.1-rc.2` 精确绑定；不匹配时拒绝进入 ready。
- 请求方法：`initialize`、`session/create`、`session/list`、`session/resume`、`session/release`、`session/rename`、`session/archive`、`session/delete`、`session/fork`、`session/history`、`session/prompt`、`session/cancel`、`session/queue`、`session/wait`、`session/working-directory/get`、`session/working-directory/set`、`model/list`、`model/select`、`settings/get`、`settings/set`、`settings/open-document`、`configuration/get`、`configuration/default-model`、`configuration/credential`、`permission/get`、`permission/select`、`approval/respond`、`question/respond`、`question/cancel`、`subagent/list`、`subagent/history`、`shutdown`。
- 通知：`session/event`、`session/assistant-stream`、`session/status`、`approval/request`、`approval/closed`、`question/request`、`question/closed`、`subagent/started`、`subagent/finished`、`host/notice`、`host/fatal`。
- UI 声明 `session.cancel`，Host 仅在双方支持时返回该 capability；未协商时调用取消返回 `protocol/capability-unavailable`。旧 Host 不声明时，UI 不展示 Stop。UI 通过 `session.history` 协商 list/resume/history，并提供原生 Sessions 入口。
- 文本流式通过 `session.assistant-stream` capability 启用；Host 只转发自己拥有的确切 agent 的公开流式帧。旧 Host/SDK 保持仅提交模式，不自动降级协议。Go 只展示 text，reasoning/工具参数不进入临时行。
- `session/create` 要求已有绝对目录 `cwd` 和显式 provider/model（可选 effort），或协商 settings.configuration 后单独传 useDefaultModel:true；两种路由不能混用。Host 验证模型路由后创建并拥有 session id。prompt 只接受自己创建或通过公开 agents.resume 恢复并拥有的会话和 text content，忙碌时拒绝新 prompt。
- 创建结果中的 `session.nextSeq` 是创建完成时 DSH 的下一事件 offset。初始会话元数据不重放；其余完整提交事件从此 offset 开始转发。`session/event` 同时携带外层 `seq` 和原始 `event.seq`，两者必须相同。
- DSH 提交事件按原样转发（含 `request/context` 等 log-only 事件）；哪些事件成为 UI 行、如何聚合为会话指标由投影层决定，Host 不替 UI 裁剪语义。
- Go 忽略重复事件；实时缺口触发 resync（标记 `resyncing`、拉取 `session/history` 补齐、按序合并）；无法安全恢复或载荷非法时停止 Host 并报错。
- 旧 SDK 的 `contentBlocks`、点分隔通知和隐式会话创建只在显式 `-sdk-protocol` 模式下启用，不自动降级。
- UI 声明 `approval` / `question`，Host 返回双方交集；未协商时对应交互不可用，不能调用回答/取消提问方法。无 agent 提问使用空 `sessionId`，不冒用当前会话身份。阻塞提问与 timed/continued 提问都通过同一请求/回答通道。
- `settings.configuration` 与 configuration/get/default-model/credential 在 Host 服务可用时协商；协议只允许固定默认模型与官方 provider 绑定的凭据配置，不暴露明文读取。通用设置通过 `settings/get` / `settings/set` 的命名空间白名单读写。
- `session.permissions` 在公开权限预设、sandbox/approval 读取和持久化服务可用时独立协商，不依赖 model.selection；未协商不调用 permission/get/select。

## 子代理只读查询

`subagent.inspection` 独立协商，要求公开 `subagents.listChildren` 与 `sessionQuery.observeSession` 同时可用；旧 Host 不声明时 UI 隐藏入口。它是协议 v1 的增量能力，不授权 `session/resume` 子代理或向子代理发送消息。

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `subagent/list` | `{parentSessionId}` | `{parentSessionId,children:[{id,label,mode,status,createdAt}]}` |
| `subagent/history` | `{parentSessionId,childSessionId,beforeSeq?}` | `{parentSessionId,childSessionId,startSeq,endSeq,nextSeq,inheritedEventCount,hasMore,events}` |

- 父会话必须由本 Host 持有、无元数据写入或释放预占；读取允许父任务运行。id 只接受 1–128 字符的 ASCII 字母、数字、`_.:-`。目录只返回公开目录中该父会话的直接子代理，不递归扩展；最多 128 条，label 最多 1024 UTF-8 bytes 且无控制字符。mode 为 `one-shot|continuable|external|unknown`，status 为 `running|idle|stored|external`；stored 仅表示没有本进程活动句柄，不推断任务结果。
- 每次读取先重新校验直接父子关系。历史通过公开 `observeSession(childId,{signal,projectionMode:'none'})` 获取，校验 header 的确切 id、parentSession 和 subagent origin；不恢复 Agent、不加入父会话所属表、不读取私有日志文件。external/unknown 没有本地历史。观察句柄在成功、失败或取消后都释放。
- offset 为非负安全整数，范围 `[startSeq,endSeq)`，页内 seq 连续升序；`nextSeq` 是本次观察的尾部边界，`inheritedEventCount` 是 fork 继承前缀的结束。缺省读取最新页，`beforeSeq` 读取该 offset 之前的页，不能跨过继承边界；`hasMore == (startSeq > inheritedEventCount)`。每页最多 64 个事件，结果最多 256 KiB 或配置帧上限一半，取较小值；超限时缩短较旧部分，单个事件仍超限则显式报错。每次更早页重新观察，不承诺跨请求固定尾部；返回的 endSeq 必须等于请求 beforeSeq。
- 历史只传白名单展示字段：assistant text、真实 user source 的 text、工具 callId/name/arguments 与 result 的关联 id/text/error.reason，以及 turn/start 与固定枚举 turn/end 原因。移除 reasoning、stream 副本、请求配置、完整 header、descriptor 与任意 provider metadata；其余事件变为仅含 seq/time 和空 data 的 `mygo/hidden`，保持边界可校验。此规则只作用于子代理查询，不改变主会话事件语义。
- 查询为提交事件的只读快照，客户端手动刷新，不订阅子代理 token 流。界面只保留一页投影；Host 的公开冷观察服务自身可能恢复完整日志，不承诺其冷读取内存只与页大小相关。
- 同时最多 8 个查询、每次 15 秒中止预算；查询预占父会话的目录读取计数，阻止句柄释放。退出先同步中止恢复任务，再中止并等待子代理读取，随后回收 Agent；UI 超时或关闭只丢弃迟到响应，RPC 请求级取消仍使用独立契约。
- 未协商返回 `protocol/capability-unavailable`；非法地址/边界返回 `protocol/invalid-params`；业务错误为 `subagent/unavailable|not-found|busy|catalog-too-large|page-too-large|read-failed`。底层异常统一为固定消息，不回传路径、凭据、请求配置或异常原文。

## 传输

传输：**stdio，newline-delimited JSON-RPC 2.0**。

- 一行一个 JSON 对象；UTF-8；不接收二进制帧。
- 请求：`{"jsonrpc":"2.0","id":1,"method":"session/create","params":{...}}`
- 响应：`{"jsonrpc":"2.0","id":1,"result":{...}}` 或 `{"jsonrpc":"2.0","id":1,"error":{"code":...,"message":"...","data":{...}}}`
- 通知：`{"jsonrpc":"2.0","method":"session/event","params":{...}}`
- 最大帧长为 `16 MiB`（包含换行）：流式通知落地后，提交事件仍保留内嵌完整 stream，不能缩小上限后丢弃持久记录。Go 用 `-max-frame`，Host 用 `mygo-channel.config.maxFrameBytes` 配置，范围需一致。
- 不接受空行、无换行的末帧、非法 UTF-8、非法 id 或混合请求/响应结构；`params` 仅接受对象。id 为不超过 128 字符的非空字符串或安全整数。
- 非法 JSON、未知帧结构、超长帧：关闭连接，Host 退出；UI 报协议错误。

多窗口或调试端口需要时，可在同一消息集上增加 loopback WebSocket transport；消息语义不变。

## 握手

### `initialize`

```json
{
  "protocolVersion": 1,
  "client": { "name": "dsh-mygo", "version": "0.1.0", "platform": "win32", "locale": "zh-CN" },
  "capabilities": ["session.cancel", "session.history", "session.assistant-stream", "approval", "question", "attachments", "model.selection", "settings.read", "settings.configuration", "session.delete"]
}
```

响应：

```json
{
  "server": { "name": "dsh-mygo-host", "version": "0.1.0", "dshVersion": "0.2.1-rc.2" },
  "protocolVersion": 1,
  "capabilities": ["session.cancel", "session.history", "session.assistant-stream", "approval", "question", "attachments", "model.selection", "settings.read", "settings.configuration", "session.delete"],
  "attachments": {
    "image": {
      "mediaTypes": ["image/png", "image/jpeg", "image/webp", "image/gif"],
      "maxBase64Bytes": 8388608,
      "maxImageBytes": 6291456,
      "maxImagesPerMessage": 8,
      "maxMessageImageBytes": 6291456,
      "maxImagePixels": 64000000,
      "maxImageDimension": 8192
    }
  },
  "permissionModes": ["workspace-write", "danger-full-access"],
  "models": [ ... ]
}
```

规则：

- `protocolVersion`、客户端 release 或 DSH 版本不兼容时，Host 返回错误并退出；UI 显示版本不匹配。协议/能力校验通过后才发送 `session/create`。
- capability 取双方交集；UI 不调用未协商的方法。
- `initialize` 之前只接受 `initialize` 与 `shutdown`。
- 重复 `initialize` 返回 `already-initialized`。
- `attachments` 能力只在 Host 确实挂载附件存储且能校验模型输入能力时协商；协商成功必须同时返回 `attachments.image`，未协商成功必须不带该字段。UI 校验上限字段为正整数、媒体类型为上述四种的子集，不合法时按握手失败处理。

## 方法

### 会话

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `session/list` | `{ cursor?, limit?, includeArchived? }` | `{ sessions: SessionSummary[], nextCursor? }` |
| `session/create` | `{ cwd, provider, model, effort? }` 或 `{ cwd, useDefaultModel:true }`（后者需 settings.configuration） | `{ session: SessionSummary }` |
| `session/resume` | `{ sessionId, provider, model, effort? }` 或 `{ sessionId, useDefaultModel:true }`（后者需 settings.configuration） | `{ session: SessionSummary }` |
| `session/release` | `{sessionId}`（需 `session.release`） | `{}`，释放所属空闲 Agent 和单写者句柄，不删除日志 |
| `session/rename` | `{ sessionId, title }`（需 `session.rename`） | `{ session: SessionSummary }` |
| `session/archive` | `{ sessionId, archived? }`（需 `session.archive`；默认 true，false 取消归档） | `{}` |
| `session/delete` | `{ sessionId }` | `{}`（需要 capability `session.delete`） |
| `session/fork` | `{ sessionId, messageId? }` | `{ session: SessionSummary }` |
| `session/history` | `{ sessionId, cursor?, limit?, direction?, beforeSeq?, live?, assistantStream? }`（backward 需 `session.history-window`；live:true 需 `session.resync`；assistantStream:true 需流式 baseline 能力） | `{ events: SessionEvent[], startSeq?, nextCursor?, hasMore, nextSeq, status?, statusRevision?, assistantStream? }` |
| `session/prompt` | `{ sessionId, content: ContentBlock[] }`（image 块需 `attachments`） | `{ messageId }` |
| `session/cancel` | `{ sessionId }`（需 capability `session.cancel`） | `{ cancelled: boolean, status: "idle" }` |
| `session/queue` | `{ sessionId, action: "list"|"edit"|"remove"|"steer", ... }` | `{ queue: QueuedInput[] }` |
| `session/wait` | `{ sessionId }` | `{}`（回合结束后返回） |
| `session/working-directory/get` | `{ sessionId }` | `{ cwd }` |
| `session/working-directory/set` | `{ sessionId, path }` | `{ cwd }` |

`SessionSummary` 包含：`id`、`title`、`cwd`、`provider`、`model`、`effort`、`createdAt`、`updatedAt`、`status`、`parentId?`、`archived`，以及可选 boolean `metadataCached` / `metadataIncomplete`；create/resume 结果额外带 `nextSeq`。缓存标志只描述目录提示的新鲜度，不构成事件边界或模型路由。

### 会话管理

- `session.rename` 仅在公开 `sessionTitle.rename` 与持久化服务可用时协商；`session.archive` 仅在公开 `workspaceRegistry.archiveSession/unarchiveSession` 与持久化服务可用时协商。未协商调用返回 `protocol/capability-unavailable`；UI 分别依据协商结果显示操作入口。
- 重命名要求本 Host 已拥有且 idle 的会话；冷会话先通过 `session/resume` 取得单写者句柄。标题为非空字符串，最多 1024 UTF-8 字节，拒绝 ASCII 控制字符，规范化由 DSH 标题服务完成。通过公开服务追加 `session/title`，等待 persistence.flush 后返回摘要及 nextSeq；不修改模型路由、用户消息或 UI 本地历史。
- 归档及取消归档接受本 profile 可见的根/分叉会话；未知 id 返回 `session/not-found`，subagent 与待迁移会话拒绝操作。归档状态写入 DSH workspace registry，不写入会话 header 或本地 UI 文件，不删除日志；重复操作幂等。默认列表隐藏归档会话，`includeArchived:true` 包含两种状态；列表游标继续首次捕获的固定快照。
- 归档会话不得 resume 或 prompt，返回 `session/archived`；先用 `session/archive` 的 `archived:false` 取消归档，再恢复或继续发送。本 Host 已拥有的归档会话保留句柄和历史读取能力。
- 已拥有会话必须 idle，且无恢复、模型或元数据写入任务；元数据任务持有会话预占直到底层写入及落盘完成，其间 prompt/resume/history/model-select 与第二次元数据写入返回 busy。配置写入与元数据写入互斥；Host shutdown 等待已接受的元数据任务结算后再释放句柄。不以超时冒充底层写入已取消；写入失败要求客户端刷新确认。
- 归档采用 registry 的活动检查，不隐式停止正在运行的任务；跨进程活动检测沿用 DSH registry 提供者的范围，不承诺跨进程 Agent 状态的全局锁。所有底层异常转换为固定错误消息，不回传标题内容、存储路径或原始异常。

### Host 历史能力

- `session.history` 同时保护 list/resume/history/fork；只有公开 sessionPersistence 的 list/stat/open/flush 和 agents.resume 可用时才协商，未协商调用返回 `protocol/capability-unavailable`。
- list 范围为当前 DSH_HOME/profile 持久化服务可见的根/分叉会话，不按 cwd 筛选；排除 subagent 和 migration-required。仅投影白名单元数据，不转发完整 header、revision、凭据或请求配置。按 updatedAt 倒序、id 升序建立固定快照。
- 目录使用自有 `mygoCatalog` 纯投影，通过公开 sessionProjections/sessionProjectionCache 保存与读取独立检查点；标题折叠最后一个 `session/title`，`updatedAt` 折叠创建/事件时间的最大值。列表不打开正文，活跃会话读取实时投影，冷会话读取身份与版本校验后的检查点并标记 metadataCached。缺失、损坏或不兼容的派生行只回退该会话的短 id/创建时间并标记 metadataIncomplete；打开会话后由 DSH 重放日志并重建检查点，不用一个大会话阻塞完整目录。冷缓存可落后于日志，不宣称精确最近活动；归档取 registry，不伪造事件时间。无投影服务的兼容 Host 可保留有界日志扫描路径。冷会话 status 为 stored，路由字段为空，不宣称其未被其他进程占用。
- 冷恢复要求显式 provider/model（可选 effort），或协商后的 useDefaultModel:true；继续使用持久 header.cwd（必须仍为已有绝对目录）。通过 agents.resume 获取单写者句柄，恢复/修复由 DSH 完成，不自行补造事件或扫描私有 JSONL。外来进程占用、未知会话、迁移或路由失败不暴露底层错误。subagent 不可恢复。
- 已拥有且 idle 的 resume 幂等返回原有路由，不重新配置模型；正在恢复/运行返回 session/busy。恢复预占 maxSessions 名额（与 create 共用，默认 8）；失败释放，退出中止待恢复任务并回收迟到句柄。
- `session.release` 与 `session.history-window` 独立协商，均依赖 session.history。release 幂等接受未拥有的合法 id；所属会话必须 idle、无审批/提问、取消、历史/目录/权限读取、模型/元数据/配置写入和未确认权限。释放期间持有预占，先 flush 与检查点写入，再通过公开 workspace/session-activity 检查后台工作；有活动时返回 busy 并解除预占，不停止子代理或 jobs。无活动才 drain 后代与 await dispose，成功才删除名额和本会话游标。派生检查点写入失败可降级为缓存提示，句柄释放失败保留名额和禁止其它操作的预占，只允许重试 release 或重启。shutdown 等待释放任务，不重复释放已完成句柄。
- UI 在下次导航或管理操作前释放非当前、非目标的退休句柄，保留紧邻返回的会话；失败打开或后台重命名得到的句柄也进入退休集合。原会话在目标历史完成前保持所属，清理失败保留当前正文/草稿。释放后再次打开属于冷恢复，使用当时默认或显式启动路由，不保证保持已释放 Agent 的内存路由。
- history 可读本 Host 拥有的会话：普通请求要求 idle；双方协商 `session.resync` 后，`session/history` 可传 `live:true` 读取运行中会话的持久日志。首页同步捕获 `nextSeq`、`status`（包含已接受但未被 driver 领取的 prompt）及 `statusRevision`，游标固定这三个值和 live 模式；后续分页不得混用模式。模型/元数据写入期间仍拒绝读取。每次使用公开 persistence.flush 建立可读屏障，read handle 在成功/失败均关闭；不调用已弃用的同步 session 事件读取方法。运行中的临时流通过可选 `assistantStream` baseline 续接，不把临时帧当历史。
- 首页捕获 agent.session.seq 为 nextSeq；forward 固定读取 `[0,nextSeq)`。backward 从捕获的 nextSeq 或显式 beforeSeq 向前读取，返回升序连续的 `[startSeq,end)`，hasMore 表示 startSeq > 0，下一游标继续更早的页并保持原 nextSeq。beforeSeq 为不超过捕获 nextSeq 的非负安全整数，仅 backward 首页可传，不能与 cursor 混用；它是持久的 append-only offset，可在游标过期后继续读取更早页。实时接续总以 nextSeq 为准，不把 startSeq 或旧页末端作为新事件边界。缺失/损坏返回 session/history-failed，不跳过事件。
- list/history 的 limit 默认为 64，合法范围 1–256（null 不是默认值）；history 默认 forward，backward 必须协商 session.history-window。单页 JSON 数组预算为配置 maxFrameBytes 的一半；反向页优先保留靠近 end 的事件，并据实际页长推进 startSeq。单条超限返回 session/page-too-large，不截断原始日志；baseline 计入页预算。
- 游标是连接内不透明 UUID，绑定方法/会话/快照边界；最多保留 32 个，5 分钟失效，重启全部失效；过期/淘汰/伪造返回 session/invalid-cursor，客户端须重新开始分页。列表快照最多 1000 条且 256 KiB，超出返回 session/catalog-too-large，不静默漏项。
- Go 在协商历史窗口时，打开、重启和 resync 仅读取最新最多 64 条事件；显式加载更早页同样最多 64 条，单次输入预算 32 MiB，不限制整份日志的事件数。读取在后台，60 秒超时；失败不发布半份页面。显示缓存与淘汰规则见 [会话投影](session-projection.md#历史回放)。列表仍限制 1 MiB/1000 条输入；旧 Host 的 forward 完整回放保留 32 MiB/20000 事件/1024 页预算。
- Go 校验摘要字段、类型、字符串长度与安全整数；历史页需明确 events/hasMore/nextSeq，逐条验证 seq/time/type/data，禁止缺口、边界变化、重复游标、空页停滞与多余末页游标。未知事件可通过序列校验，但由投影忽略。
- 目标会话加载期间的 durable/status/交互通知有界缓冲（1024 条/8 MiB），不从零散临时帧猜测历史或 baseline；运行中的临时流以 Host 同步快照续接。成功切换后按原顺序从历史 nextSeq 衔接，重复 durable 事件丢弃。溢出、实时缺口或无法安全处理的并发会话活动触发 resync；resync 失败才停止 Host，不猜测或静默丢失真相。

### 模型与设置

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `model/list` | `{ sessionId, provider? }`（需 model.selection） | `{ providers: {id,name}[], provider, models: {id,name,available,efforts:{id,name}[],inputModalities?,reason?}[], selection:{provider,model,effort}, available, reason? }` |
| `model/select` | `{ sessionId, provider, model, effort? }`（需 model.selection） | `{ session: SessionSummary }` |
| `configuration/get` | `{}`（需 settings.configuration） | `ConfigurationSnapshot` |
| `configuration/default-model` | `{revision,provider,model,effort?}`（需 settings.configuration） | `ConfigurationSnapshot` |
| `configuration/credential` | `{reference,operation:"set",value}` 或 `{reference,operation:"unset"}`（需 settings.configuration） | `ConfigurationSnapshot` |
| `permission/get` | `{sessionId}`（需 session.permissions） | `PermissionSnapshot` |
| `permission/select` | `{sessionId,preset,expectedNextSeq,confirmDangerous?}`（需 session.permissions） | `PermissionSnapshot` |
| `settings/get` | `{ namespace? }` | `{ values }`（只返回非 secret） |
| `settings/set` | `{ namespace, values }` | `{ values }` |
| `settings/open-document` | `{ namespace }` | `{}`（由 Host 打开原生编辑器） |

模型目录和当前会话选择通过 model.selection 协商；独立的 settings.configuration 保护固定范围的持久默认模型与凭据配置；通用设置走 settings/get/set 的命名空间白名单，不接受未声明命名空间写入。目录、范围、资源限制和凭据边界见 [设置与凭据](settings-credentials.md)。model/select 的 effort 省略或空字符串均代表清除显式 override；成功提交一个 model/selection 事件并返回新的 nextSeq，不修改历史消息或全局默认值。`inputModalities` 只在模型元数据声明时出现，取值为 `text`/`image` 的子集并去重；缺少该字段表示输入模态未知，UI 不据此禁用发送，Host 也不预先拒绝图片，仍由模型适配器在调用时决定。

ConfigurationSnapshot 为 `{revision,defaults:{provider,model,effort},credential:{available,reference,configured,writable,source}}`，只包含安全白名单字段，Go 输入预算 8 KiB。revision 为非负安全整数；路由/引用最多 256 UTF-8 bytes；引用必须为 POSIX identifier；source 仅 none/managed/environment/fallback。凭据值仅出现在 set 请求（1–8192 bytes、无 ASCII 空白/控制字符），任何结果或错误都不能回传。默认模型写入使用 revision 乐观冲突检查；凭据 reference 必须等于 Host 当前官方 provider 的配置绑定。写入失败后必须 configuration/get 确认，未结束的写入不被读操作或下一写入越过；不承诺 RPC 超时取消底层持久化。

`PermissionSnapshot` 为 `{sessionId,current,sandbox,approval,nextSeq,options:[{id,name,description,sandbox,approval,requiresConfirmation}]}`。sandbox 只允许 read-only/workspace-write/danger-full-access，approval 只允许 ask/never；nextSeq 为非负安全整数。选项最多 32 且身份唯一；id/name/current 最多 256 UTF-8 字节，description 最多 2048 字节；快照最多 32 KiB 且不超过配置帧上限一半，不携带根路径或完整配置。custom 可为当前值，不能成为选项或切换目标；非 custom 的当前 bundle 必须与目录一致。

permission/select 仅接受所属 idle 会话，无并发模型/元数据/配置写入；expectedNextSeq 必须等于当前会话边界，否则返回 permission/conflict。目标实际 sandbox 为 danger-full-access 或 approval 为 never 时 requiresConfirmation 为 true，必须传 boolean confirmDangerous:true，否则返回 permission/confirmation-required；名称不参与风险判断。公开 set 后等待 flush，成功返回实际快照，幂等选择可保持原 nextSeq。写入保留会话预占直至底层结束；失败返回脱敏 permission/write-failed，之后 permission/get 成功确认前拒绝 prompt/导航/下一写入。读取等待尚未结束的权限写入，不以请求超时取消底层任务。具体所有权和 UI 确认规则见 [设置与凭据](settings-credentials.md#权限模式)。

### 交互

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `approval/respond` | `{ requestId, decision: "allow"|"deny" }`（需 `approval` capability） | `{}` |
| `question/respond` | `{ requestId, answers: { id, selected: string[], custom? }[] }`（需 `question` capability） | `{}` |
| `question/cancel` | `{ requestId }`（需 `question` capability） | `{}` |

`approval/respond` 与 `question/respond` 必须幂等：重复回答同一个 `requestId` 返回 `stale-request`，不改变已决结果。

审批和提问请求身份只由 Host 生成，回答只接受当前连接待处理的身份。审批不支持 `remember`。迟到/重复回答或取消返回 `approval/stale-request` / `question/stale-request`，不改变已决结果。问题/答案的校验、有界队列与作用域见 [交互](interaction.md)。

### 生命周期

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `shutdown` | `{}` | `{}`，随后 Host 退出 |

## 通知

| 通知 | 载荷 | 说明 |
| --- | --- | --- |
| `session/event` | `{ sessionId, seq, event }` | DSH session log 事件 |
| `session/assistant-stream` | `{ sessionId, frame }` | process-local assistant 帧：start / chunk / end |
| `session/status` | `{ sessionId, status: "running"|"idle" }` | 整回合状态 |
| `approval/request` | `{ requestId, sessionId, agentId, toolName, reason, external, callId?, arguments? }` | 需要用户决定；参数只来自活跃提交工具调用 |
| `approval/closed` | `{ requestId, sessionId }` | 请求已决或撤销；可能先于回答 RPC 响应到达 |
| `question/request` | `{ requestId, sessionId, questions }` | 需要用户回答 |
| `question/closed` | `{ requestId, sessionId }` | 已回答或撤销；可能先于 RPC 响应到达 |
| `subagent/started` | `{ parentSessionId, childSessionId, provider? }` | 子代理创建 |
| `subagent/finished` | `{ parentSessionId, childSessionId, provider?, reason }` | 子代理结束；`reason` 为 `completed`/`aborted`/`error`/`max-tokens`/`refusal` |
| `host/notice` | `{ level, message }` | 非致命提示 |
| `host/fatal` | `{ message, diagnostic? }` | 致命错误，随后退出 |

协商 `session.resync` 后，`session/status` 额外携带非负安全整数 `statusRevision`：每个被拥有 Agent 的状态通知使其单调递增，显式取消返回 idle 时同样递增。`live:true` 的每个历史页返回同一快照的 `status/statusRevision`；客户端以此作为状态基线，丢弃 revision 不高于基线的迟到通知。新连接/新 Agent 的版本空间独立，不跨 Host 比较；未协商不要求该字段。

`subagent/started|finished` 只在子代理的父会话由本 Host 持有时转发；父/子 id 与 provider 都经过标识符白名单校验，`reason` 取固定枚举（未知值归为 `error`）。通知只暴露生命周期与身份，不携带子代理消息、工具参数或结果正文。

### `session/assistant-stream`

```ts
type AssistantStreamFrame =
  | { type: 'start'; attemptId; revision; turn; step }
  | { type: 'chunk'; attemptId; revision; index; time; chunk }
  | { type: 'end'; attemptId; revision; index
      outcome: { kind: 'committed'; eventType: 'assistant/message' | 'assistant/attempt'; seq }
             | { kind: 'abandoned' } }
```

- `revision` 在一次 Agent attach 生命周期内单调；每次 attach 从 1 开始。Host 不替换正在运行的 agent；Go 保留该 Agent 的 revision 高水位并忽略重复/迟到帧。新 Host 或切换 Agent 时重置高水位，同一 Host 的 resync 不重置。
- `chunk.index` 从 0 开始、密集递增；UI 发现 revision/index 缺口时清除当前 attempt，并在协商流式 baseline 后自动 resync；旧 Host 等待下一次 `start` 或提交事件。
- 真实 DSH 先提交对应 `seq` 的 `assistant/message` / `assistant/attempt`，再发送 `end.committed`，不是结束帧先到。Go 在同 turn/step 的提交到达时替换/清理草稿，迟到 end 不恢复旧行；也容忍 end 先到，保留前缀直到对应提交、idle 或退出，不把 end 本身当历史。
- `end.abandoned` 表示没有提交事件；UI 丢弃该 attempt 的草稿。
- 临时状态最多 128 个块、8000 个可见 Unicode 字符；超长文本只截断显示。帧结构/类型非法使连接失败；块关联异常清除临时 attempt，提交日志仍按原有 seq 校验继续。

### 流式 baseline

- 独立 capability `session.assistant-stream-baseline` 依赖 `session.resync` 与 `session.assistant-stream`。协商后，`session/history` 的 `live:true, assistantStream:true` 请求同时捕获持久边界、状态版本与流式快照；未协商或非 live 请求携带 true 返回 capability-unavailable。省略/false 保持原结果；标志必须为 boolean，游标绑定两个模式，不得跨模式复用。
- 快照形状为 `{revision, activeAttempt?:{attemptId,turn,step,nextIndex,blocks:[{index,type,text?,ended}],truncated}}`。`revision` 为非负安全整数；activeAttempt 缺省表示没有可续接的临时流（不改变回合 running/idle），不能为 null。其余计数为安全整数，nextIndex 是下一 chunk offset 且小于 revision。块按出现顺序排列，index 不重复；text 只属于 text 块。ended/truncated 必须为 boolean。
- Host 只从公开 `agent/assistant-stream` 累计文本和块边界，不读取 Agent 私有字段，也不保留 reasoning、工具参数或完整帧列表。文本最多 8000 rune，最多 128 块；文字超限标记 truncated，块超限或关联错误清除 attempt 直到下个 start。匹配 turn/step 的持久 assistant 提交及 turn/end 同步清除 attempt，避免提交与 end 之间捕获重复预览。
- 历史首页在第一次 await 前同步捕获快照；后续页返回同一份 baseline，不能混入之后的 chunk。baseline JSON 最多 128 KiB，并从历史单页事件预算中扣除，超限或剩余预算无法容纳单事件时明确返回 page-too-large；仍计入 Go 的历史总字节预算。连接内游标数量/有效期沿用历史规则。
- Go 在后台严格校验、整体恢复 revision/块状态/nextIndex；不接受低于本 Agent 已见版本的快照。缓冲流只应用更高 revision，整体发布前校验连续衔接；新缺口与持久缺口共用最多 3 次重读和 60 秒预算，非法 baseline 走失败恢复。提交事件替换临时文本，不把 baseline 写入历史。

## 事件顺序

- 每个 session 的 `session/event` 带单调递增的 `seq`。
- 同一 session 的 `session/status` 与 `session/event` 按 Host 发出顺序到达；Go 不重排。
- 跨 session 不保证全局顺序；UI 按 session 分开处理。
- Go 发现 `seq` 缺口时：
  1. 标记该 session 为 `resyncing`，暂停发送；
  2. 调用 `session/history` 拉取缺口范围并校验边界；
  3. 合并后继续消费实时事件；失败则停止连接并提示重启。
- `session.resync` 依赖 `session.history`；旧 Host 未协商时保留手工重启恢复，不发送 live 请求。自动补齐期间保持当前投影、草稿和交互队列，暂停发送/导航/设置，隐藏交互弹窗；协商历史窗口后重建尾部，否则从 seq 0 回放固定前缀。后续 durable/status/stream/交互通知及未完成取消的 UI 确认在同一有界队列中按接收顺序保留（1024 项/8 MiB）。未知会话通知不混入当前会话。
- 全部历史页成功且缓冲持久事件能够连续衔接后，整体替换投影并回放通知；重复 durable 事件丢弃，状态仅接受更新的 revision，旧 RPC 确认不能覆盖最新 Host 状态。新缺口使客户端重新捕获快照，最多 3 次，共用 60 秒预算；非法页/通知、缓冲溢出或预算耗尽停止 Host 并显示恢复。模型/配置/会话元数据写入交叉时走失败恢复，不冒充已撤销写入。
- 临时流在补齐开始时清除，协商 baseline 后随快照整体恢复，并从快照 revision/nextIndex 衔接缓冲；未协商时缺少完整 start 就等待下一 start 或持久提交。已被快照包含的 assistant 提交不能因迟到 end/临时帧重新显示第二份答案；最终 idle 不保留临时行。
- 禁止把不连续的事件拼成“当前状态”。

## 取消与超时

### 回合取消

- Host 只接受自己拥有的 session，调用公开 `Agent.cancel({ kind: "user" })`，默认清空待处理输入，然后等待 `Agent.whenIdle()`。会话不会销毁，工具/模型的 abort signal 由 DSH 传播。
- 空闲取消返回 `{ cancelled: false, status: "idle" }`，不预先取消未来回合；活跃回合终止后返回 `{ cancelled: true, status: "idle" }`。取消期间的并发请求共用同一任务；再次取消等同空闲取消。
- Host 的 `cancelTimeoutMs` 默认 5000（可通过 channel config 配置）；未能确认 idle 时返回 `session/cancel-timeout`，不伪造成功，也不改变日志。Go 请求超时为 10 秒；失败后仍保持 working 并允许重试，最终 idle 由 Host 通知确认。
- Go 在 prompt 被 Host 接受后才发出取消，避免立即点击 Stop 时取消越过 prompt。取消期间保留下一条草稿并禁用重复点击；必须等取消请求返回且 Host 确认 idle 后才重新允许发送。提前收到 idle 只记录状态，不解除发送屏障，避免迟到的取消请求误停新回合。旧回合的迟到取消响应不能改变新回合。
- 取消提示来自 Host 提交的 `turn/end`（`aborted`），UI 不自行生成历史。若回合恰好已经完成，则保留实际完成结果。

### RPC 请求级取消

独立于回合取消：

- 每个请求有 `id`；取消用 `$/cancel` 通知：`{ "id": <requestId> }`。
- `session/cancel` 取消当前回合，不取消“列出会话”这类请求。
- 请求超时由 Go 侧计时；超时后发送 `$/cancel`，并把 UI 操作标记为失败。
- 取消是尽力而为：Host 可能已经完成操作；响应中要说明实际结果。

## 错误

遵循 JSON-RPC 2.0：`error.code` 是整数，应用错误用 `data.kind` 细分。

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "error": {
    "code": -32000,
    "message": "Session is running",
    "data": { "kind": "session/busy", "sessionId": "..." }
  }
}
```

| `data.kind` 前缀 | 含义 |
| --- | --- |
| `protocol/*` | 帧、版本、未知方法、参数 |
| `session/*` | 会话不存在、忙、恢复失败、附件无效 |
| `model/*` | 模型/effort 不可用 |
| `approval/*` | 请求过期、已决、未知 |
| `question/*` | 同上 |
| `host/*` | 内部失败、不可用 |

标准 JSON-RPC 错误码：`-32700` parse、`-32600` invalid request、`-32601` method not found、`-32602` invalid params、`-32603` internal。应用错误使用 `-32000..-32099`，并以 `data.kind` 区分。

规则：

- 错误消息面向用户，可本地化；诊断信息放 `data`。
- 不把 Host stderr、路径、token 直接暴露到 UI 主错误文案。
- 未知 `data.kind` 按 `host/*` 处理，但保留原始值供日志。

## 附件

图片随 `session/prompt` 的 content 块发送；Host 在追加 user 事件前把字节提交为持久附件，会话日志只保存引用与元数据。

```json
{ "type": "image", "mimeType": "image/png", "data": "<canonical base64>", "name": "shot.png" }
```

- `type` 只有 `text` 与 `image`，未知类型返回 `protocol/invalid-params`；`text` 最多 1 MiB、允许空串，但整条消息至少要有一个非空 text 块或一个 image 块；单条消息最多 64 个块。
- `mimeType` 必须属于握手公布的媒体类型；`data` 必须是 canonical base64（无空白、无 URL-safe 变体、填充只在末尾），长度不超过 `maxBase64Bytes`。
- 图片按顺序提交：单图编码字节数 ≤ `maxImageBytes`，全部图片编码字节数 ≤ `maxMessageImageBytes`，数量 ≤ `maxImagesPerMessage`；任何一项超限返回 `session/attachment-invalid`，不产生部分写入。
- `name` 可选，最多 128 个字符，不含路径分隔符和控制字符；只作显示名，不解释为路径。
- Host 先校验当前路由的输入模态（`inputModalities` 未声明视为未知并放行，声明为 text-only 时拒绝），再把图片交给 DSH 附件服务提交；提交失败返回 `session/attachment-invalid`，消息不含 base64、存储路径或原始异常。
- 会话日志与投影只出现 `attachment.attachmentId / mediaType / bytes / width / height / name?`；UI 不通过历史读取 base64，也不把附件写入本地草稿文件。
- 帧预算：单帧仍受 `maxFrameBytes` 约束，图片以 base64 计数；UI 发送前按协商上限校验并保留草稿，Host 再按部署限制收紧。
- 文件引用（`@path`）走 text 块的路径引用，不新增协议方法；Host 与 UI 同机，路径由 UI 显式选择。
- 不在日志、崩溃报告或错误文本中打印图片字节、base64 或文件路径中的敏感片段。

## 背压

- Go → Host：UI 在 busy 时禁用新 prompt 或排队；Host 顺序处理请求，并通过 `maxSessions`（默认 8）限制会话数量。
- Host → Go：通知按接收顺序进入同一有界批处理队列，16 ms、64 项或累计 256 KiB 时发布一批；最后一帧受单帧上限约束，可使该批超过字节阈值。最多等待 32 个 UI 批次，满时暂停读取 stdout；不静默丢弃事件或 token。Host 写队列由 `maxQueuedBytes`（默认 32 MiB）限制；超限关闭连接，使 UI 进入失败状态。
- RPC 结果的 UI 回调也经过同一队列，不能越过已经接收的提交通知或 idle。尤其取消响应不得提前解除发送屏障，让旧取消/idle 影响下一回合。每批只生成一次可见 transcript 快照，不逐 token 重建列表。
- 只影响 UI 的派生通知（例如百分比进度）可以合并/丢弃；持久事件不可以。

## 版本化

- `protocolVersion` 是整数；破坏性变更 +1。
- 新方法/通知通过 capability 声明；旧 Host 不声明时 UI 不调用。
- 删除方法时先保留一个版本的兼容路径，再在新 major 中移除。
- Host 与 UI 的发布版本绑定；协议版本是额外保险，不是独立升级承诺。

## 测试

- 协议 conformance：合法帧、非法帧、未知方法、版本不匹配、取消、重复回答。
- 顺序测试：乱序/缺口 resync、历史回放与实时事件衔接。
- 背压测试：大事件流、慢 UI、附件上限。
- 真实 Host 集成：用 `dsh --profile mygo` 跑一个回合，验证 approval/question 往返。

## 相关文档

- [宿主监督](host-supervisor.md)
- [会话投影](session-projection.md)
- [交互](interaction.md)
- [进度与路线](../roadmap.md)
- [传输方案研究](../research/transport-options.md)
