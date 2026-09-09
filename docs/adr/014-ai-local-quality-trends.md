# ADR-014：AI 本地质量分组与时间趋势

- 状态：Accepted，AI7-Q2 本地趋势读模型已实现
- 日期：2026-09-09
- 决策范围：评测状态统计、同口径质量分组、最近完整 Run 趋势
- 相关文档：[ADR-013](013-ai-local-quality-evaluation-runner.md)、[AI 助手](../modules/ai-assistant.md)、[AI7 计划](../plans/ai-quality-gates.md)

## 背景

ADR-013 已让用户显式运行并保存无正文的本地模型评测历史。单条 Run 可以解释当次 4 个 case 的结果，但仅按历史列表人工查看，无法回答以下问题：

- 相同模型快照在同一数据集上累计通过了多少 case；
- 更换本地模型后，结果是否发生变化；
- 最近若干次完整运行的通过比例如何变化；
- 基础设施失败、取消或仍在运行的 Run 是否被错误混入质量分数。

数据集、Provider 名称和模型名都可能变化。把所有 Run 合成一个“总通过率”会制造不可比较的数字，因此趋势必须先冻结分组口径，再决定哪些状态可以进入分子和分母。

## 决策

### 1. 只从本地无正文 Run 读取

- `GET /api/v1/ai/evaluation-summary` 从 schema 063 的 `ai_evaluation_runs` 即时聚合，不新增物化表、后台任务或线上服务。
- 可选 canonical `provider_id`；不存在的 Provider 返回 404。`limit` 默认 12、范围 1–50，仅控制最近完整 Run 的趋势点数量。
- 读取状态计数、质量分组和趋势点使用同一个 SQLite 只读事务，避免同一响应跨快照。
- 响应不读取 `ai_evaluation_results` 正文——该表本来也没有回答；不返回 prompt、question、chunk、answer、reasoning、URL、key 或费用。

### 2. 状态统计与质量统计分离

- `status_counts` 统计全部匹配 Run：queued、running、succeeded、failed、cancelled，总数必须严格对账。
- 只有 `status=succeeded` 的完整 Run 进入质量分组与趋势。failed 是执行基础设施失败，cancelled 是用户终止，queued/running 尚无完整结果；三者都不能进入通过比例。
- Run succeeded 仍不等于全 case 通过。`fully_passed_runs` 只计 `failed_cases=0` 的完整 Run；质量分子/分母分别为 `passed_cases` 与 `total_cases`。

### 3. 不跨口径混算

质量分组键固定为：

```text
provider_id
+ provider_name_snapshot
+ provider_model_snapshot
+ dataset_version
```

- Provider 重命名、模型切换或数据集升级都会形成独立组，不追溯改写历史。
- API 不返回跨组总通过率；前端只在每个组内计算整数百分比，并同时显示原始 `passed/total` 与 `fully_passed_runs/run_count`。
- 最近趋势点携带同一组身份、Run ID、dataset version、passed/failed/total 和完成时间。API 先取最近 N 条，再按时间正序返回供界面绘制。
- dataset v1/v2 历史分别固定为 4/12 个 case；ADR-020 当前 dataset v3 固定为 24 个。v1/v2/v3 必须分别展示，不允许为了连续图形把分母不同的点混成一条无标识曲线。

### 4. Web 再验证和可视化

- Web 对 `status_counts` 求和、质量组 `passed+failed=total`、组 Run 数合计等于 succeeded 数、分组唯一、趋势点唯一/正序/归属已有组做失败关闭校验。
- 设置页显示完整运行、运行失败、取消和活动计数；每个同口径组显示 case 通过比例和全通过 Run 比例。
- 最近趋势使用轻量本地条形图，同时标注模型、dataset version、`passed/total` 与整数百分比。视觉长度不是唯一信息来源，读屏树仍包含完整数字。
- 创建、取消和删除评测后失效趋势缓存；存在活动 Run 时每 500 ms 本地轮询，终态后停止。

## 威胁与控制

| 威胁                      | 控制                                                              | 验证         |
| ------------------------- | ----------------------------------------------------------------- | ------------ |
| failed/cancelled 混入分数 | 状态单独计数；质量 SQL 只读 succeeded                             | API/Web 测试 |
| 不同模型或数据集混算      | 四字段分组键；趋势点携带完整组身份                                | API/Web 测试 |
| 百分比掩盖小样本          | 同时显示 passed/total 与 fully-passed/run-count                   | UI 测试      |
| 趋势时序不稳定            | 最近 N 条按 completed_at/id 截取，响应按 completed_at/run_id 正序 | API/Web 测试 |
| 聚合泄露模型回答          | 只读 Run 固定列；API/Web 禁止正文、知识、URL 与 key 字段          | 契约测试     |
| 聚合快照内部不一致        | 单只读事务；客户端重算状态、组和趋势不变量                        | API/Web 测试 |
| 删除历史后显示旧趋势      | 终态删除使 Query 同时失效历史、详情和 summary                     | Hook/UI 测试 |

## 被拒方案

1. **所有模型合并为一个总分**：模型快照和数据集版本不同，比例不可比较。
2. **把运行失败当 case 未通过**：混淆基础设施可用性与模型回答质量。
3. **只显示百分比**：小样本 100% 容易误导；必须保留原始计数。
4. **保存每日物化统计表**：当前数据量小，即时只读聚合更简单且删除后自然一致。
5. **远程上传或排行榜**：违反本地服务边界，也会引入隐私和成本问题。
6. **跨 dataset version 连接一条曲线**：分母和题目变化后会制造虚假连续性。

## 后果与后续

- 优点：用户能在不查看或保存模型回答的情况下比较本地模型快照，并区分质量变化与运行故障。
- 代价：Provider 重命名也会形成新组，这是保留历史展示口径的有意选择；当前最多展示最近 12 点，API 上限 50。
- 后续：ADR-015 category、ADR-020 dataset v3、ADR-018 Wilson 区间与 ADR-019 人工评审候选已交付；增加事实集时仍须升级 dataset version。分层套件和时间范围筛选继续独立评审。趋势只辅助本地决策，不自动切换 Provider、不自动阻止聊天，也不成为云端遥测。
