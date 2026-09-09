# ADR-018：AI 本地质量 Wilson 区间与重复证据

- 状态：Accepted，AI7-Q2 描述性不确定性已实现
- 日期：2026-09-09
- 决策范围：同口径质量比例的 Wilson 区间、重复运行提示与界面解释边界
- 相关文档：[ADR-014](014-ai-local-quality-trends.md)、[ADR-015](015-ai-local-quality-categories.md)、[ADR-017](017-ai-local-quality-dataset-v2.md)、[AI 助手](../modules/ai-assistant.md)、[AI7 计划](../plans/ai-quality-gates.md)

## 背景

dataset v2 把每次完整评测扩展到 12 个 case，但直接显示 `passed/total` 或整数百分比仍容易掩盖小样本的不确定性。例如一次 v2 运行的 12/12 并不等于模型在真实业务中有 100% 成功率；每类 3/3 的证据更弱。重复运行相同 case 能观察模型输出稳定性，却不能增加事实场景的多样性，也不能假设所有结果彼此独立。

本轮需要在不保存回答、不新增统计表和不引入线上服务的前提下，让设置页明确表达这种不确定性。它不是发布门禁：不会给出“可发布/不可发布”，不会自动阻止聊天、切换 Provider、创建任务或修改业务状态。

## 决策

### 1. 对现有同口径计数计算 95% Wilson score interval

- `GET /api/v1/ai/evaluation-summary` 继续只聚合 succeeded Run，并沿用 Provider ID + 名称快照 + 模型快照 + dataset version 分组。
- 总体质量组和 category 组都返回整数 basis points：`pass_rate_bps / wilson_lower_bps / wilson_upper_bps`，范围 0–10000。
- 固定 `z = 1.959963984540054`、置信水平 9500 bps，使用双精度 Wilson score 公式后四舍五入到 1 bps。下界/上界夹在 0–10000，且必须满足 `lower <= pass_rate <= upper`。
- 响应显式携带 `uncertainty={method:"wilson_score", confidence_level_bps:9500, repeated_run_minimum:3}`。Web 必须用同一公式从原始 passed/total 重算并逐项精确核对，伪造区间或口径变化均失败关闭。

区间只描述当前固定 case outcome 在二项比例模型下的数学范围。重复运行同一 case 的相关性可能违反独立同分布假设，因此不得把区间解释为生产业务质量的统计保证。

### 2. 重复证据只描述运行次数

每个总体质量组新增代码所有 `evidence_level`：

- `single_run`：1 次完整 Run；
- `limited_runs`：2 次完整 Run；
- `repeated_runs`：至少 3 次完整 Run。

该字段只说明相同模型快照与 dataset version 被完整重复观察了多少次。它不表示样本已经充分、不提高事实覆盖、不成为自动阈值。category 继承父组的 Run 口径，不重复保存 evidence level。

### 3. 设置页保留计数并解释区间

- 总体卡继续显示 `passed/total`、全通过 Run 数和点估计；新增 `95% lower–upper` 与“单次观察/有限重复/重复观察”。
- category 行继续保留分子/分母，在其下显示对应 Wilson 区间；条形长度仍只表达点估计。
- 说明文案明确：“重复观察”从 3 次完整 Run 开始，95% Wilson 区间只描述固定样本不确定性，不代表发布许可。
- 桌面与 390 px 窄屏均须保留所有精确数字；颜色、条形和标签都不是唯一信息来源。

### 4. 无迁移、无后台任务

- 所有值在既有 evaluation summary 读请求中即时计算，不新增 schema、物化统计、定时器或 Actor 消息。
- 删除评测历史后区间随下一次读取自然变化；failed/cancelled/queued/running Run 仍不进入比例或区间。
- API 仍不返回问题、知识 chunk、回答、reasoning、failure detail、Provider URL、key、费用或用户业务正文。

## 威胁与控制

| 威胁                        | 控制                                                       | 验证         |
| --------------------------- | ---------------------------------------------------------- | ------------ |
| 100% 小样本被误读为确定结论 | 同时展示原始分母和宽 Wilson 区间                           | API/UI 测试  |
| 服务端区间或口径被篡改      | basis-point 整数；Web 按固定公式精确重算 method/level/区间 | Web 契约测试 |
| 不同模型或 dataset 混算     | 继续沿用四字段质量组键                                     | API/Web 测试 |
| 重复运行被称为事实覆盖充分  | evidence 只命名运行重复程度；文案说明不增加事实多样性      | ADR/UI 文案  |
| 运行故障污染统计            | 只读取 succeeded Run；状态统计继续独立                     | API 测试     |
| 新统计扩大隐私或网络面      | 仅从无正文计数计算；无表、无持久化、无网络                 | 代码/测试    |

## 被拒方案

1. **普通正态区间 `p ± 1.96*SE`**：在 0/1、1/1、3/3 等边界小样本会退化或越界。
2. **只显示点估计**：无法表达 3/3 与 30/30 虽同为 100%，证据宽度不同。
3. **把 repeated_runs 命名为“已验证”或“可信”**：重复同题不等于覆盖新事实，也不是生产质量保证。
4. **立即加入 pass/fail 发布阈值**：阈值、最低类别要求、严重失败码和处置动作仍需独立决策。
5. **保存每次计算结果**：即时计算简单，持久化会产生删除同步与第二事实源。

## 后果与后续

- 优点：用户能看到点估计背后的样本宽度；12/12 的一次运行会显示明显宽于多次重复运行的区间。
- 代价：Wilson 区间仍依赖二项模型，固定 case 的重复结果可能相关；界面需要解释而不能只显示一个“置信分”。
- 后续：ADR-019 已确定最低 Run 数、总体/category 下界与严重 failure code，并交付只读人工评审候选；人工覆盖理由与本地审计仍待独立评审。自动阻止聊天、自动切换 Provider 或对外发布继续不在授权范围内。
