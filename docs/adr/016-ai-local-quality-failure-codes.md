# ADR-016：AI 本地质量失败原因聚合

- 状态：Accepted，AI7-Q2 failure-code 统计已实现
- 日期：2026-09-09
- 决策范围：同口径模型组内的稳定失败原因、影响 case 数与出现次数
- 相关文档：[ADR-013](013-ai-local-quality-evaluation-runner.md)、[ADR-014](014-ai-local-quality-trends.md)、[ADR-015](015-ai-local-quality-categories.md)、[AI 助手](../modules/ai-assistant.md)

## 背景

ADR-015 已能说明哪个 category 较弱，但同一 category 可能因不同原因失败：缺少必要事实、出现禁止结论、引用越权或引用集合不符。schema 063 Result 的 `failure_codes` 是代码所有、无正文 JSON 数组，可以进一步聚合；但一个 case 可以同时拥有多个 code，同一 code 也可能因多条缺失短语在同一 case 内出现多次。

如果只用 `COUNT(*)`，会把重复 code 误称为多个失败 case；如果只去重，又会丢失一个失败模式反复出现的强度。因此需要同时保留“影响了多少 case”和“总共出现多少次”。

## 决策

### 1. 只展开完整质量 Run 的失败 Result

- `GET /api/v1/ai/evaluation-summary` 在既有只读事务中，对 `status=succeeded` Run 下 `result.status=failed` 的 `failure_codes` 使用 SQLite `json_each` 展开。
- failed/cancelled/queued/running Run 不参与；passed Result 的空数组不产生行；`status=error` 属于基础设施问题，不混入质量失败原因。
- failure code 必须命中 `aieval` 代码所有白名单。非字符串、未知 code 或不合法聚合使服务端失败，不把损坏状态展示为业务事实。

### 2. 保留两种计数

每个 failure-code 分组返回：

- `affected_cases = COUNT(DISTINCT result.id)`：至少包含该 code 的 failed case 数；
- `occurrences = COUNT(*)`：该 code 在所有数组中出现的总次数；
- `last_completed_at`：该模型组中最近出现该 code 的完整 Run 时间。

必须满足 `1 <= affected_cases <= occurrences`，且 affected cases 不得超过父模型组的 `failed_cases`。每个父组所有 code 的 affected cases 之和可以大于 failed cases，因为同一 case 可有多个不同原因；但不得小于 failed cases，因为 schema 063 要求每个 failed Result 至少一个 failure code。

### 3. 不跨质量组合并

failure-code 分组键为：

```text
provider_id
+ provider_name_snapshot
+ provider_model_snapshot
+ dataset_version
+ failure_code
```

- Provider 重命名、模型切换或数据集升级后形成独立组。
- API 不返回跨组“最常见原因”排名，避免把不可比较的模型/数据集混在一起。
- Web 校验父组存在、failure-code 唯一、白名单、计数上下界和最小覆盖后才展示。

### 4. 设置页展示

- 每个存在 failed case 的模型/dataset 组显示“失败原因”。
- 每个原因显示稳定中文标签、`影响 N case · 出现 M 次` 和以 affected/failed 控制的条形长度。
- 条形图不是唯一信息来源；精确计数始终可见。
- 不展示模型回答、知识正文、failure detail、必需/禁止短语、URL、key 或费用，也不自动生成修复建议。

## 威胁与控制

| 威胁                           | 控制                                                        | 验证         |
| ------------------------------ | ----------------------------------------------------------- | ------------ |
| 重复 code 被误算成多个 case    | affected 使用 DISTINCT result.id；occurrences 单独保留      | API 测试     |
| 一个 case 多原因导致总数超分母 | 不把各 code affected 相加当唯一失败 case 数                 | ADR/UI 文案  |
| 未知或损坏 code 被展示         | 服务端与 Web 双重白名单                                     | API/Web 测试 |
| failure 跨模型/dataset 混算    | 四字段父组键 + failure code                                 | API/Web 测试 |
| 部分/错误 Run 污染质量原因     | 只展开 succeeded Run 的 failed Result                       | API 测试     |
| 失败原因泄露正文               | 只返回稳定 code 与计数，禁止 answer/question/content 等字段 | API/Web 测试 |

## 被拒方案

1. **只返回 occurrences**：无法区分同一 case 内重复与多个 case 共同失败。
2. **只返回 affected cases**：丢失同一失败规则多次命中的信息。
3. **把所有 failure code 合成全局榜单**：跨模型和数据集不可比较。
4. **返回 failure detail 或命中短语**：会扩大评测内部文本的持久化/响应面。
5. **自动据此阻止模型使用**：当前样本量不足，发布阈值尚未授权。

## 后果与后续

- 优点：category 说明“哪类能力弱”，failure code 进一步说明“为何未通过”，同时保持无正文和同口径边界。
- 代价：一个 case 多原因时各行 affected 不能简单相加；界面必须同时保留父组 failed case 总数。
- 后续：ADR-017 已提供更大的版本化数据集，ADR-018 已提供描述性 Wilson 区间，ADR-019 已明确只读人工评审候选的最低 Run/category 下界与严重 code，ADR-022 已提供不可变人工决定审计；任何自动发布门禁仍待。当前统计和人工记录都不自动切换 Provider、不生成任务或改变业务状态。
