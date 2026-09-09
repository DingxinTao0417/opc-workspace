# ADR-015：AI 本地质量类别分解

- 状态：Accepted，AI7-Q2 category 统计已实现
- 日期：2026-09-09
- 决策范围：同口径模型组内的评测 category 聚合与展示
- 相关文档：[ADR-013](013-ai-local-quality-evaluation-runner.md)、[ADR-014](014-ai-local-quality-trends.md)、[AI 助手](../modules/ai-assistant.md)、[AI7 计划](../plans/ai-quality-gates.md)

## 背景

ADR-014 已提供同 Provider/模型快照/dataset version 的总体 case 比例和最近 Run 趋势，但总体比例无法解释失败集中在哪类能力。例如同样是 75%，可能是引用注入防护持续失败，也可能是偶发的资料不足判断失败。schema 063 的每个无正文 Result 已保存代码所有 `category`，因此可以在不读取或保存模型回答的前提下进行更细分的本地诊断。

category 统计仍不能成为自动发布门禁。历史 dataset v1/v2 每类分别为 1/3 个 case；ADR-020 当前 dataset v3 扩展到每类 6 个，仍必须展示原始分子/分母，不能仅以百分比制造确定性。

## 决策

### 1. 直接聚合 succeeded Run 的 Result

- `GET /api/v1/ai/evaluation-summary` 在 ADR-014 同一只读事务中，JOIN `ai_evaluation_runs` 与 `ai_evaluation_results`。
- 只有所属 Run 为 `status=succeeded` 的 Result 进入 category 统计；failed/cancelled/queued/running Run 的部分或空结果不进入。
- category 只允许 schema 063 已冻结的 `grounded / no_evidence / prompt_injection / conflicting_sources`。
- 每个 category 返回 passed/failed/total 与最近完成时间；不聚合 `status=error`，因为 succeeded Run 按契约没有 error case。

### 2. category 必须留在同一质量组内

category 分组键是 ADR-014 四字段组键再加 category：

```text
provider_id
+ provider_name_snapshot
+ provider_model_snapshot
+ dataset_version
+ category
```

- 不跨 Provider、名称快照、模型快照或数据集版本合并同名 category。
- 每个四字段质量组的所有 category 合计必须同时满足：category total = group total、category passed = group passed、category failed = group failed。
- API 按固定 category 顺序返回；Web 仍检查唯一性、归属和三项合计，不能把缺失 category 当作零分或静默忽略。

### 3. 界面保留原始计数

- 设置页在每个模型/dataset 组下显示四类能力的 `passed/total` 和轻量条形图。
- 百分比只控制条形长度，不作为唯一文案；读屏树与可视界面都保留类别名称和精确分子/分母。
- 总体趋势、模型组和 category 使用同一 Query；创建、取消、删除和手工刷新同步更新。
- category 统计不展示问题、知识片段、模型回答、failure detail、URL、key 或费用。

## 威胁与控制

| 威胁                        | 控制                                               | 验证         |
| --------------------------- | -------------------------------------------------- | ------------ |
| 部分失败 Run 污染类别比例   | 只 JOIN succeeded Run                              | API 测试     |
| category 跨模型或版本混算   | 四字段组键 + category                              | API/Web 测试 |
| category 缺失仍伪造完整总分 | category 三项合计必须与父组一致                    | Web 测试     |
| 小样本百分比造成误导        | 显示 passed/total，不显示独立无分母分数            | UI 测试      |
| 恶意响应重复或跨组 category | Web 检查 category 唯一键与父组存在性               | Web 测试     |
| 统计接口泄露评测正文        | 固定无正文列；沿用 evaluation summary 禁止字段检查 | API/Web 测试 |

## 被拒方案

1. **全局 category 排行榜**：跨模型和 dataset version 的同名 category 不可直接比较。
2. **把 failed Run 的 error case 算作 category 失败**：这是基础设施问题，不是回答质量。
3. **只画百分比、不显示样本量**：当前每类样本很少，必须同时显示分母。
4. **按 failure code 代替 category**：failure code 是失败原因，一个 case 可有多个；category 是测量维度，两者不能互换。
5. **保存回答以解释 category**：首片继续坚持无正文结果；用户需要原回答时应显式运行新的诊断流程并另行评审。

## 后果与后续

- 优点：用户能定位本地模型在有依据回答、资料不足、引用注入防护和冲突来源中的相对薄弱项，同时保留模型/数据集口径。
- 代价：summary 多一次小规模 Result 聚合；历史删除后 category 会随下次读取自然更新。
- 后续：ADR-016 failure-code、ADR-020 dataset v3、ADR-018 Wilson 区间、ADR-019 人工评审候选及 ADR-021–023 分层套件/人工决定审计/四类专题套件已交付；继续改变事实集必须递增 dataset version。不得依据当前 24-case 固定样本、专题结果或人工记录自动切换模型或阻止用户聊天。
