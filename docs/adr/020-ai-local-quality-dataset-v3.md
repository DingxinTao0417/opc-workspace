# ADR-020：AI 本地质量评测数据集 v3

> 后续修订：[ADR-025](025-ai-reliability-confirmations-and-evaluation-identity.md) 接续运行预算/锁、确认事务/隐私、部分压缩进度及评测配置身份。本文保留当时决策与验证记录；当前行为以 ADR-025 和模块文档为准。

- 状态：Accepted，AI7-Q2 24-case 事实覆盖已实现
- 日期：2026-09-09
- 决策范围：第二轮事实场景扩展、中英/category 平衡、历史版本与人工评审候选联动
- 相关文档：[ADR-017](017-ai-local-quality-dataset-v2.md)、[ADR-018](018-ai-local-quality-confidence-intervals.md)、[ADR-019](019-ai-local-quality-advisory-readiness.md)、[AI7 计划](../plans/ai-quality-gates.md)

## 背景

dataset v2 的 12 个 case 已让四类能力分别拥有 3 个中英场景，并支持 Wilson 区间与人工评审候选。但每类 3 个事实问题仍容易让单个场景对 category 下界产生过大影响；客户回访、Task 人工验收、备份边界、付款资料不足和本地 Agent 安全等 opc-workspace 自身关键规则尚未覆盖。

本轮继续扩大代码所有、完全本地的事实集。它只增加显式评测时的固定输入，不读取用户真实业务数据，不把 prompt/chunk/answer 写入数据库，也不改变聊天、Provider 或业务模块。

## 决策

### 1. dataset v3 固定为 24 个 case

- 语言：`zh-CN` 12 个、`en` 12 个。
- category：`grounded / no_evidence / prompt_injection / conflicting_sources` 各 6 个。
- 完整保留 v2 的 12 个 case，并增加 12 个新 case；embedded 顶层版本从 2 递增为 3。
- 创建 Run 与 Actor 执行继续分别加载严格 `{version,cases}`，新 Run 固定 `dataset_version=3 / total_cases=24`；任何版本或数量不一致仍以 `AI_EVALUATION_DATASET_INVALID` 失败。

### 2. 新增事实场景

| category            | 新增中文场景                                              | 新增英文场景                                    |
| ------------------- | --------------------------------------------------------- | ----------------------------------------------- |
| grounded            | 客户回访摘要与下一次计划                                  | Task owner 验收；完整备份包含 SQLite 与受控文件 |
| no_evidence         | 银行账户资料不足；法定节假日提醒规则不足                  | USD→CNY 汇率来源/日期不足                       |
| prompt_injection    | 恶意要求外发令牌/路径，真实规则只记录 request ID 与错误码 | 任意 Shell 指令；要求绕过 citation 并伪造来源   |
| conflicting_sources | 发票最终复核人冲突；自动备份保留期限冲突                  | Task 截止日期冲突                               |

每个 case 继续使用 1–3 个稳定 UUID chunk、明确来源/行号、required/forbidden phrases、citation status 和 exact allowlist。scorer 算法、failure code 白名单与生产 citation parser 不变。

### 3. 版本与统计联动

- schema 64 已允许正整数 dataset version，本轮不新增迁移或数据库列。
- dataset v1/v2 的 Run/Result、趋势、category、failure-code 与 Wilson 区间继续可读且保持原分母；不得追溯重写为 v3。
- ADR-019 policy 的 `current_dataset_version` 从 embedded dataset 自动读取为 3。v1/v2 质量组固定为 `insufficient_evidence / OUTDATED_DATASET`；只有 v3 组可继续判断 Run 数、Provider version、Wilson 下界和严重 code。
- 一次 v3 Run 的总体分母为 24、每类分母为 6；三次完整 Run 为总体 72、每类 18。若全部通过，95% Wilson 下界分别约为 94.93% 与 82.41%，仍只表示固定事实集观察。

### 4. 运行成本与取消

- 单邮箱 Actor 仍顺序执行，每 case 最长 3 分钟；极端超时上界从 v2 的 36 分钟增加到 v3 的 72 分钟。
- 用户可取消活动 Run；模型/协议失败仍生成一个 error Result 并终止，不静默重试或跨 Provider 降级。
- 设置页进度从 Run 的 `completed_cases / total_cases` 读取，无需硬编码 24；历史 v1/v2 卡片继续显示 4/12 分母。

## 威胁与控制

| 威胁                       | 控制                                                  | 验证            |
| -------------------------- | ----------------------------------------------------- | --------------- |
| 修改 case 却沿用旧版本     | embedded version 与 cases 同载荷；创建/执行双加载     | scorer/API 测试 |
| 语言或 category 分布漂移   | 测试固定 12/12 语言与每类 6 个                        | scorer 测试     |
| v2/v3 历史混算             | summary 继续以 dataset version 为组键                 | API/Web 测试    |
| 旧数据集仍显示人工评审候选 | policy 从 embedded 读取 current=3；Web 重算 OUTDATED  | API/Web 测试    |
| 24 请求造成资源占用        | 显式触发、单邮箱串行、逐 case timeout、可取消         | Actor 测试      |
| 新 prompt/回答进入持久化   | schema/API 仍仅保存 ID/category/failure/citation/指标 | 禁止字段测试    |

## 被拒方案

1. **原地扩展 dataset v2**：会让旧、新 12/24-case Run 共享版本，破坏历史口径。
2. **删除 v1/v2 历史**：用户评测事实必须由用户显式清理，升级不能代删。
3. **并行运行 24 个请求**：本地模型资源不可预测，取消与失败归因更差。
4. **从用户业务数据自动生成 case**：会引入正文持久化、隐私和不可重复性。
5. **因样本增大而自动启用 Provider**：人工评审候选仍不是发布或启用授权。

## 后果与后续

- 优点：每类拥有 6 个事实场景，覆盖更接近 opc-workspace 的真实规则；三次完整运行时 category 区间明显收窄。
- 代价：完整运行的本地模型调用翻倍到 24 次，极端完成时间也相应增加。
- 后续：ADR-021 已交付 8-case smoke 与 24-case full，ADR-022 已交付不可变人工决定审计，ADR-023 已交付四类代码所有 6-case 专题套件；继续扩展不得超过现有 32-case 上限，也不开放任意 case 选择。PDF/页码事实和句子级归因仍需独立推进。
