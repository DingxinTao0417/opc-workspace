# ADR-021：AI 本地质量分层评测套件

- 状态：Accepted，AI7-Q2 smoke/full 套件已实现
- 日期：2026-09-09
- 决策范围：快速诊断与完整评测、Run 套件身份、统计隔离和人工评审候选资格
- 相关文档：[ADR-020](020-ai-local-quality-dataset-v3.md)、[ADR-019](019-ai-local-quality-advisory-readiness.md)、[AI 助手](../modules/ai-assistant.md)、[AI7 计划](../plans/ai-quality-gates.md)

> 后续修订：ADR-023/schema 067 在保留本 ADR 的 smoke/full 语义与历史分组前提下，增加四个代码所有 6-case 专题 suite。本 ADR 中“只允许 smoke/full”描述 schema 065 的初始基线；当前六值契约以 ADR-023 为准。

## 背景

dataset v3 包含 24 个串行 case，事实覆盖更完整，但在较慢的本地模型上可能需要较长时间。用户有两种不同需求：先快速检查模型/协议/引用防护是否基本工作，或运行完整集合以积累可比较的质量证据。若两种运行只靠 `total_cases` 猜测并进入同一模型/dataset 分组，8-case 与 24-case 的总体、category、failure-code 和 Wilson 区间都会被错误混算。

因此套件必须成为不可变 Run 身份和统计组键，而不能只是前端按钮。快速结果只用于诊断；无论运行多少次都不能形成 ADR-019 的人工评审候选。

## 决策

### 1. embedded dataset 明确定义两个代码所有套件

- `full`：dataset v3 全部 24 个 case，中英各 12、四类各 6。
- `smoke`：8 个固定 case，中英各 4、四类各 2，覆盖 grounded、no-evidence、prompt-injection 和 conflicting-sources。
- JSON 顶层新增严格 `suites[]`，每项只含 `key / case_ids`。只允许 `smoke/full`，suite/case 不可重复、不得引用未知 case，顺序必须与 dataset 一致。
- `full` 必须逐项覆盖全部 case；`smoke` 必须恰好 8 个并保持语言/category 平衡。非法定义使整个 embedded dataset 不可用。

当前 smoke 顺序选择：发票归档、退款资料不足、英文引用注入、付款条款冲突、英文 Task 验收、英文汇率资料不足、中文秘密外发注入、英文截止日期冲突。

### 2. schema 65 固化 Run 套件

- `ai_evaluation_runs.suite_key` 为非空 `smoke/full`，默认 `full`。v1–v3 既有 Run 都是当时完整数据集执行，因此迁移默认值保持历史解释，不改 Result。
- 新增按 Provider/名称/模型/dataset/suite/status/completed_at 的质量读索引；迁移只增列/索引，不需要 destructive gate。
- Run API、列表、详情和历史全部返回 `suite_key`；未知持久值或响应值失败关闭。
- 两张评测表仍排除便携业务导出并由一致性 SQLite 备份覆盖。schema 65 不改变业务表列；v49→65、v63→65、v64→65 兼容边显式维护。

### 3. 创建、幂等与 Actor

- `POST /api/v1/ai/evaluations` 接受 `suite_key=smoke|full`；缺省仍规范化为 `full`，兼容旧客户端。
- suite 进入规范化请求摘要和 Idempotency-Key 语义。同 Provider/version/suite 的失败重试复用 key；用户切换 suite 时 Web 生成新 key，避免异体重放冲突。
- 创建时从 embedded dataset 解析 suite，分别固化 `total_cases=8/24`。Actor 执行前再次以 Run 的 dataset version + suite key 重建 case 序列，并同时匹配数量；任何漂移以 `AI_EVALUATION_DATASET_INVALID` 结束。
- 每 Provider 仍最多一个 queued/running Run；smoke 与 full 不能并行争用本地模型。

### 4. 所有质量统计按 suite 隔离

evaluation summary 的质量组键扩展为：

```text
provider_id
+ provider_name_snapshot
+ provider_model_snapshot
+ dataset_version
+ suite_key
```

- parent、category、failure-code、trend 与 Web 唯一键全部携带 suite；8/24 分母绝不相加。
- Provider version min/max 继续在每个 suite 组内计算。
- ADR-019 policy 新增 `required_suite=full`。当前 dataset 的 smoke 组固定返回 `insufficient_evidence / SUITE_NOT_ELIGIBLE`，先于 Run 数、Wilson 下界和严重 code 判断。
- 旧 dataset 仍先返回 `OUTDATED_DATASET`；full 组继续按 3 Run、Provider version、总体/category 下界与严重失败码判断。

### 5. 设置页交互

- ready/healthy local Provider 显示“快速质量检查”和“完整质量评测”两个明确按钮；帮助文本分别说明 8-case 诊断和 24-case 完整证据。
- 任一评测活动时两个入口都禁用；完整按钮显示活动状态，不让用户误以为可以并发。
- 质量卡、趋势、category、failure 和 Run 历史均显示“快速/完整”；快速组明确显示“快速套件只用于诊断”。
- 桌面与 390 px 窄屏必须保留按钮、套件、分母、区间和候选原因，不以颜色作为唯一信号。

## 威胁与控制

| 威胁                        | 控制                                                       | 验证              |
| --------------------------- | ---------------------------------------------------------- | ----------------- |
| smoke/full 比例被混算       | suite 持久化；所有 SQL/Web 组键加入 suite                  | API/Web 测试      |
| 快速检查被误当候选证据      | policy required_suite=full；SUITE_NOT_ELIGIBLE             | API/Web/UI 测试   |
| 客户端伪造或遗漏 suite      | API 只接受两值、缺省 full；数据库 CHECK；Web 严格解析      | 迁移/API/Web 测试 |
| 切换套件复用旧幂等键        | Hook 按 Provider/version/suite identity 保留或轮换 key     | Hook 测试         |
| 数据集升级后 suite 引用漂移 | suites 与 cases 同一 embedded 载荷，双加载版本/数量        | scorer/Actor 测试 |
| 两个套件并行争用本地模型    | 每 Provider 单活动 Run 唯一索引                            | API 测试          |
| 快速路径扩大网络或正文面    | 仍只调用显式本地回环 Provider；schema/API 不保存题目或回答 | 边界测试          |

## 被拒方案

1. **仅用 total_cases 推断套件**：未来数据集版本变化后 8/24 不再是稳定身份，也无法防止伪造。
2. **smoke 结果与 full 合并**：测量对象不同，比例和区间不可比较。
3. **smoke 达标即可进入人工评审**：每类只有 2 个固定 case，目的只是快速诊断。
4. **允许自定义 case 列表**：会破坏可重复性、版本口径和代码所有安全边界。
5. **并行运行 smoke/full**：本地模型资源不可预测，且每 Provider 单活动约束已经清晰。

## 后果与后续

- 优点：用户可以先用 8-case 快速发现明显问题，再按需运行 24-case 完整评测；所有历史和统计仍可解释。
- 代价：summary 分组维度增加，模型/dataset 下会出现多张套件卡；客户端与文档必须始终显示 suite。
- 后续：ADR-022 已交付精确质量组快照上的不可变人工决定审计，ADR-023 已交付四类代码所有 6-case 专题套件。若 case 超过 32，应新增显式 suite/dataset 版本而不是开放任意选择；PDF/页码套件及句子级归因仍需独立推进。
