# ADR-025：AI 运行可靠性、确认事务与评测配置身份

- 状态：Accepted；实现与验收进度见 [AI7 计划](../plans/ai-quality-gates.md)。
- 日期：2026-09-10
- 范围：AI 独立轨道可靠性修复；不授权业务工具、自动路由、PDF、自主 Agent 或子代理产品能力。
- 接续：[ADR-004](004-ai-assistant-provider-access.md)、[ADR-005](005-agent-harness-and-local-models.md)、[ADR-006](006-harness-matrix-memory-evolution.md)、[ADR-007](007-session-context-compaction-and-memory-tools.md)、[ADR-019](019-ai-local-quality-advisory-readiness.md)、[ADR-020](020-ai-local-quality-dataset-v3.md)、[ADR-022](022-ai-local-quality-human-review-audit.md)。这些文件保留当时的决策，本 ADR 修订下述契约。

## 背景

审查基线为 `feature/ai-assistant` / `34b197c`。既有单元测试没有覆盖跨维护锁等待、流尾帧、超大回合、刷新重试和临时会话工具路径。组件内 task ID 不能提供服务端幂等；Provider 的 HTTP 行版本也不能代表稳定模型配置。修复必须覆盖真实 Harness、HTTP/SSE、领域事务及前端恢复，不以模拟成功替代落库事实。

## 决策

### 1. 运行隔离与预算

- 模型网络等待不持有全局 maintenance 或 Provider 锁；准备/注册、数据库事务和收尾仍在短维护锁内，且重验 `restorePending`。取消控制不排队等待 maintenance 写锁。
- 禁用的备份扫描不获取全局写锁；启用后仍在受保护阶段重验策略。安排恢复时取消活动生成、压缩和评测，但不得持锁等待这些任务退出。
- 每次 generation 的模型轮、工具输出、自检修订共享 10 分钟和累计 1 MiB 预算。8 轮、32 次工具调用、单工具 30 秒/64 KiB 等上限仍独立存在；不能因进入下一轮重新获得预算。
- 前后端都要求明确流终态。OpenAI `stop` 后继续读 usage；只有内容而无完成信号的 EOF 为协议失败，`length`/过滤与取消不能伪装成功。Anthropic 要求 `message_stop`。缺失用量保持 unknown，不估算 token 或费用。
- selfcheck 示例和解析器使用同一闭合格式；缺失/非法为 `SELF_CHECK_UNAVAILABLE`，不记录为自检通过。不充分最多修订一次；明确不足但无可修订正文时仍记录 `SELF_CHECK_INSUFFICIENT`，不启动空修订、不伪装通过；修订失败按失败保留已有部分回答。

### 2. 应用级恢复与草稿

- 生成归应用级内存状态管理。SPA 切页可继续生成，全局保持可见停止入口；重新进入 AI 页恢复同一运行。
- 完整 SSE 终态回答在应用内存保留，最多 20 回合且正文/思考/用户输入合计 8 MiB，超限淘汰最旧回合；持久历史按 generation 去重，删除会话同步清除。没有持久消息身份的回合只读，不伪造消息 ID 或调用任务/记忆确认 API。
- 恢复查询以命令代次隔离，旧回读与 404 清理不能覆盖后来发送的请求。非持久终态没有服务端正文时，已收片段按 `incomplete` 展示；服务重启后回读 404 也保留应用内片段/思考，不冒充完整成功。
- 浏览器只持久化 request/session 等恢复标识，不保存 prompt、回答或草稿。刷新/断连会终止原 SSE 连接，上游取消与终态保存可能有短暂竞态；客户端通过活动/单 generation/按 request key 查询恢复真实状态，不自动重发一条新消息。
- `POST /ai/chat` 的稳定 `Idempotency-Key` 与输入摘要存入 generation。已接受请求重试返回 `AI_CHAT_ALREADY_ACCEPTED` 及 generation/session ID；不同输入复用 key 拒绝。Sidecar 重启仍使用原有 interrupted/cancelled 恢复语义，不承诺恢复模型推理过程。
- 接受前失败保留输入和上下文选择；恢复旧草稿不能覆盖后来输入。输入法 composition 的 Enter 不发送。硬刷新不保留纯内存草稿，这是隐私边界而非持久草稿功能。

### 3. 人工确认是可回读的领域命令

- `POST /ai/messages/:id/task-confirmation` 使用 Task 创建字段，在一个事务中调用共享任务创建领域逻辑并绑定消息。消息 ID 为稳定确认身份；重复同载荷返回已有 Task/完整 Message，改载荷冲突，Task 已删除返回 410，不创建替代任务。
- 复用标题/日期/标签/项目/父级校验、父级协调及其事件。普通 Task 创建当前没有 `task_created` 事件，本次不虚构事件或绕过领域规则。旧 `/messages/:id/task` 只保留静态挂接兼容，不再作为新 UI 的两步创建路径。
- 未确认明确“尚未创建”；已创建名称和入口取服务端事实。模型控制文本和自然语言都不能当作成功凭证。解析器对任意 JSON 值、坏字段、重复/未闭合块失败关闭，历史异常消息也不得使页面崩溃。
- 记忆提议使用 durable `decision` 与 `memory_id`。GET 可回读 pending/confirmed/rejected；重复忽略幂等，确认后不能用“忽略”撤销，删除长期记忆须显式使用记忆删除 API。无法从历史事件辨明结果的旧 superseded 提议保留未知，返回 `409 AI_MEMORY_DECISION_UNAVAILABLE`，不冒充已忽略。重放确认缓存仍检查真实记忆是否存在，已删除返回 410。
- 旧的无 proposal_id 卡片通过消息派生。GET 不落库；明确确认/忽略时才以稳定消息身份 materialize 记录。确认或忽略后刷新保持结果，删除后的已确认记忆不能被原卡片重新创建。模型提供的 proposal_id 必须匹配会话及实际确认内容。

### 4. 长会话与不持久会话

- 超过单批上限的回合必须分段推进，不跳过正文。快照水位线增加 `source_message_offset`：0 表示该条消息完整覆盖，正数表示剥离控制块后的 UTF-8 正文已覆盖字节数。部分消息不计入“已压缩 N 条”。
- 快照替换与水位线推进同事务；失败、取消、配置变化保留上次成功水位线，可重试，不把滞后或失败写成全部完成。
- `persist=false` 的消息、generation 正文、会话事实、模型记忆提议及摘要均不落库。记忆工具只使用当前运行共享内存；临时提议不发持久 proposal_id。永久记忆是另外一次用户明确确认，不由工具直接写入。
- 临时正文也不得进入日志、事件、idempotency 响应缓存或前端 storage；运行指标只保留无正文元数据。

### 5. 评测的身份与证据层级

- `ai_providers.version` 继续保护 ETag 并发；`config_version` 只在协议、端点、模型、类型或实际凭据变化时递增。健康检查不拆分同配置证据。
- Run/Review 保存 nullable `provider_config_version`，质量、类别、失败、趋势和人工审计按配置身份隔离。旧历史无法安全重建配置，保持 NULL 和原审计，不冒充当前配置、不要求删除旧历史才能积累新证据。
- 数据集升级 v4，保持 24 个 case 及 6/8/24 套件；增加数据驱动事实变体、否定/冲突回归和 `FACT_CONTRADICTED`。结构校验、词语匹配、有限事实规则和人工判断分层；规则不是通用语义证明。
- 评测使用生产系统提示及真实 Harness 的可重复测试。自动化只用隔离 mock/回环夹具，不调用付费或外部模型。回答级 `validated` 只证明引用身份在本次 allowlist 内，不证明句子级支持或事实正确。

## 迁移与恢复

- schema 068：Provider 配置身份、Run/Review nullable 身份；为扩展严重失败码约束受保护重建 Review，逐列保留旧审计并恢复不可变触发器。已有数据库通过受保护迁移/回滚包流程升级。
- schema 069：generation 请求身份、消息确认摘要、记忆决定与部分压缩水位线；既有请求身份留 NULL，已有完整水位线 offset=0。根据既有脱敏确认事件回读历史 proposal→memory，不改写审计事件。
- 不新增 AI 表，仍为十张；新增列继续排除便携业务 JSON/ZIP。兼容图显式增加 v67/v68→69，并沿用已允许的 v49/v63/v64/v65/v66 导入关系；未知未来 schema 拒绝。
- 单个迁移事务失败不推进该版本。迁移/恢复按既有已验证回滚包流程处理，不对用户库手工删表或重置。

## 验收及限制

验证记录统一写入 [AI7 计划](../plans/ai-quality-gates.md)，接口及当前功能以 [模块文档](../modules/ai-assistant.md) 为准。基线 5 个 Automation Event/业务导入顶层失败需与新增回归分列。真实模型质量、原生 WebView 输入法、真实断网/父进程崩溃仍需单独实机验收；自动化通过不等于这些场景已经验证。
