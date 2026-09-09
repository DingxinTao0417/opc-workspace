# ADR-017：AI 本地质量评测数据集 v2

- 状态：Accepted，AI7-Q2 多样本数据集已实现
- 日期：2026-09-09
- 决策范围：代码所有评测数据集版本、样本扩展、旧历史兼容与迁移边界
- 相关文档：[ADR-013](013-ai-local-quality-evaluation-runner.md)、[ADR-014](014-ai-local-quality-trends.md)、[ADR-015](015-ai-local-quality-categories.md)、[ADR-016](016-ai-local-quality-failure-codes.md)、[ADR-018](018-ai-local-quality-confidence-intervals.md)、[AI7 计划](../plans/ai-quality-gates.md)

## 背景

dataset v1 只有 4 个 case，每个 category 恰好一个样本。它足以验证本地 Actor、无正文 Result、citation 与 scorer 金链，但不适合据此设置发布阈值或把一次失败概括为整个能力类别。ADR-015/016 已明确要求先扩大版本化事实集，再讨论置信区间或自动门禁。

本轮仍遵守本地优先边界：评测只由用户显式触发，只调用 ready/healthy 的本地回环 Provider；不会上传数据、自动切换模型、自动阻止聊天或保存问题、知识正文和模型回答。

## 决策

### 1. 数据集版本与内容共同由代码拥有

- 内置 JSON 从匿名 case 数组升级为严格对象 `{version,cases}`，未知字段、尾随数据、非正版本、空 case 集和超过 32 个 case 均拒绝。
- 创建 Run 和 Actor 执行时分别读取同一 embedded dataset；Run 固化 `dataset_version` 和 `total_cases`。Actor 再次读取后必须同时匹配版本与数量，否则以 `AI_EVALUATION_DATASET_INVALID` 失败，不向错误版本写 Result。
- 以后任何会改变题目、知识片段、期望短语、citation allowlist 或 case 集合的改动都必须递增版本；不得在相同版本下静默改写测量对象。

### 2. dataset v2 固定为 12 个平衡 case

- 语言：`zh-CN` 6 个、`en` 6 个。
- category：`grounded / no_evidence / prompt_injection / conflicting_sources` 各 3 个。
- 场景覆盖发票归档与开票复核、项目交付、税率/支持期限资料不足、两类引用注入，以及付款、保留期和审批责任冲突。
- 每个 case 仍只有 1–3 个代码所有知识 chunk，使用稳定 UUID、位置与来源名；scorer 继续只执行确定性短语、citation status、allowlist、去重和集合校验。

dataset v2 的 12 个串行请求最多分别使用既有 3 分钟 case timeout；用户可取消，单邮箱 Actor 防止并行争用本地模型资源。扩大样本不会改变 Provider 选择、协议、Harness、citation parser 或无正文持久化边界。

### 3. schema 64 保留 v1 历史

- schema 64 在受保护的 destructive migration 中共同重建 `ai_evaluation_runs/results`，把 `dataset_version = 1` 放宽为正整数，同时逐列复制所有 Run/Result。
- 迁移继续保留 Provider 外键、每 Provider 单活动 Run、Result 顺序/case 唯一、token 全有/全无、状态约束、级联删除和 Result 不可更新 trigger；迁移完成后执行 foreign-key check。
- 已有 dataset v1 的 4-case 历史保持原版本与分母；新运行只写 dataset v2/12。summary 沿既有四字段组键分开显示 v1/v2，不跨版本计算比例、category 或 failure code。
- schema 64 只调整排除于便携业务导出的 AI 操作态；v49→64 与 v63→64 的业务导入兼容不改变业务表列。

### 4. 界面不硬编码 case 数

- 设置页说明改为“当前版本化的内置中英 case”，具体版本与进度读取 Run 的 `dataset_version / completed_cases / total_cases`。
- v1 历史继续显示 4 个 case；v2 新 Run 显示 12 个，不因当前数据集升级改写旧卡片。
- 趋势、category 和 failure-code 仍显示原始分子/分母；扩大到每类 3 个样本仍不足以授权自动发布门禁。

## 威胁与控制

| 威胁                       | 控制                                                                 | 验证              |
| -------------------------- | -------------------------------------------------------------------- | ----------------- |
| 内容变化但版本未变化       | JSON 版本与 cases 同载荷；创建/执行双读取并匹配 Run 快照             | scorer/API 测试   |
| 升级后旧 4-case 历史被混算 | schema 保留 v1；summary 继续按 dataset version 分组                  | 迁移/summary 测试 |
| 表重建丢失约束或历史       | destructive + foreign-keys-off 受保护迁移、逐列复制、FK/trigger 验证 | 迁移测试          |
| 样本扩展导致并发资源争用   | 单邮箱 Actor、逐 case 串行、可取消、每 case 独立 timeout             | Actor 测试        |
| 新 case 泄露到历史或响应   | schema/API 仍只有 case ID、category、failure code 与无正文指标       | API 禁止字段测试  |
| 12-case 比例被当成发布证明 | UI 保留分母；不提供 threshold/pass gate、不切换 Provider             | 文档/UI 契约      |

## 被拒方案

1. **直接修改 v1 的四个 case**：会让同一 dataset version 表示不同测量对象，历史失去可比性。
2. **只增加 case、不升级 schema/version**：旧分母与新分母会被 summary 错误合并。
3. **把题目或知识正文写入 Run**：扩大本地敏感数据、备份与删除面，违反 ADR-013 无正文边界。
4. **一次并行运行 12 个请求**：本地模型资源不可预测，取消与错误归因更困难。
5. **立即设置自动发布阈值**：每类 3 个事实样本仍不足以支撑稳定发布决策；ADR-018 的描述性区间也不授权自动阻断。

## 后果与后续

- 优点：四类能力不再由单个 case 代表；中英覆盖平衡，v1/v2 历史仍可解释。
- 代价：一次完整运行从 4 次增加到 12 次本地模型调用，完成时间和本地资源占用相应增加。
- 后续：ADR-018 已交付描述性 Wilson 区间与重复运行标签，ADR-019 已交付最低 category 要求和只读人工评审候选，ADR-022 已交付精确证据快照上的人工决定审计。任何自动阻止聊天、自动切换 Provider 或自动创建任务仍需单独授权。
