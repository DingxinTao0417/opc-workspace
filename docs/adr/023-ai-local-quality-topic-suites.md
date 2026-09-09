# ADR-023：AI 本地质量代码所有专题套件

- 状态：Accepted，AI7-Q2 四个 6-case 专题套件已实现
- 日期：2026-09-09
- 决策范围：专题套件定义、持久身份、迁移、统计隔离、设置页入口
- 相关文档：[ADR-020](020-ai-local-quality-dataset-v3.md)、[ADR-021](021-ai-local-quality-tiered-suites.md)、[ADR-022](022-ai-local-quality-human-review-audit.md)、[AI 助手](../modules/ai-assistant.md)、[AI7 计划](../plans/ai-quality-gates.md)

## 背景

smoke 能快速发现横跨四类的明显问题，full 能积累完整候选证据，但二者都不适合定向复查某一种能力。模型调整 citation、防注入提示或冲突处理后，用户若只想验证相关类别，只能重复运行 24 个 case。开放任意 case 多选又会产生不可比较、不可命名、难以审计的分母，并让客户端选择成为质量事实的一部分。

专题套件因此必须由代码和 embedded dataset 所有。每个套件拥有稳定 key、固定 case 顺序和明确分母；它仍是诊断证据，不替代 full，也不形成发布许可。

## 决策

### 1. dataset v3 增加四个固定专题套件

不修改 24 个 case 或 dataset version，只增加四个新的 suite identity：

| suite key             | 设置页名称   | case | 约束                                        |
| --------------------- | ------------ | ---: | ------------------------------------------- |
| `grounded`            | 有依据回答   |    6 | 全部 category=grounded，中英各 3            |
| `no_evidence`         | 资料不足     |    6 | 全部 category=no_evidence，中英各 3         |
| `prompt_injection`    | 引用注入防护 |    6 | 全部 category=prompt_injection，中英各 3    |
| `conflicting_sources` | 冲突来源     |    6 | 全部 category=conflicting_sources，中英各 3 |

每个专题套件必须包含 dataset 中该 category 的全部 case，顺序与全量 dataset 一致，不得重复、遗漏、跨类别或引用未知 ID。`smoke=8` 与 `full=24` 的既有定义保持不变。suite key 不由用户命名，不开放任意 case 选择。

dataset version 仍为 3，因为问题、知识夹具和期望事实没有变化；新历史使用新的 suite key，不能与既有 smoke/full 组混算。未来若改变 case 内容或期望，仍必须递增 dataset version。

### 2. schema 67 扩展 Run 与人工审计的 suite 约束

SQLite 不能原地修改 CHECK，migration 067 因而是受保护的 destructive migration：

- 共同重建 `ai_evaluation_runs / ai_evaluation_results`，逐列复制全部历史并恢复 Provider/Result 外键、活动 Run 唯一索引、质量组索引和 Result 不可更新 trigger；
- 重建 `ai_evaluation_reviews`，逐列保留决定、理由、Actor 和证据快照，并恢复 UPDATE/DELETE 不可变 trigger；
- 三张表的 suite CHECK 统一允许 smoke、full 和四个专题 key；未知 key 继续失败；
- 启动必须先经过现有迁移备份门禁。v66 工作区在应用 schema 67 前得到可验证回滚点。

迁移没有新增便携业务表列。评测与审计表继续排除业务 JSON/ZIP；显式维护 v49→67、v63→67、v64→67、v65→67 和 v66→67 兼容边，按源版本校验历史排除清单。

### 3. 创建、Actor、幂等与统计

- `POST /api/v1/ai/evaluations` 接受六个代码所有 suite；缺省仍为 full。未知 key 返回 `AI_EVALUATION_SUITE_INVALID`。
- suite 继续进入请求摘要和 Idempotency-Key identity。失败后相同 Provider/version/suite 重试复用 key；切换任一专题会轮换 key。
- 创建与 Actor 执行分别重读 embedded dataset，以 dataset version、suite key 和 case count 双重校验；专题 Run 固定 6 case，单 Provider 仍只有一个活动 Run。
- parent、category、failure、trend、Wilson 和人工审计仍以 Provider ID/名称/模型快照/dataset/suite 为组键。专题组只含自己的一个 category，不伪造其他三类零分母。
- ADR-019 readiness 继续要求 `required_suite=full`。smoke 与四个专题统一返回 `insufficient_evidence / SUITE_NOT_ELIGIBLE`；运行次数或分数不会改变这一边界。
- ADR-022 可对专题组追加人工决定与理由。写事务只要求该组至少一个真实 category，并继续重算当前证据；人工记录仍不改变 Provider、聊天或业务状态。

### 4. 设置页交互

ready/healthy local Provider 保留“快速质量检查”和“完整质量评测”，并新增一个紧凑专题区：

- 下拉框列出四个稳定中文名称；
- “运行专题检查”启动所选 6-case suite；
- 明示“每类中英各 3 个 case；不形成候选资格”；
- 任一评测活动时，快速、完整、专题选择和专题运行均禁用；不允许并行争用本地模型；
- Run 历史、质量卡、趋势、category、failure 与人工审计使用同一 suite 标签。

交互延续现有设置页的紧凑工业化视觉，不增加新的全屏流程。桌面和 390 px 下，选择器、按钮、分母和“不形成候选”说明必须完整可读。

## 威胁与控制

| 威胁                             | 控制                                                 | 验证                   |
| -------------------------------- | ---------------------------------------------------- | ---------------------- |
| 任意 case 组合导致分母不可比较   | 只接受六个代码所有 key；embedded 严格 case 列表      | aieval/API/Web 测试    |
| 专题混入其他 category            | 专题必须覆盖且只覆盖对应 category，中英各 3          | dataset 校验测试       |
| 新 suite 与旧历史混算            | suite 是数据库列和所有统计/审计组键                  | migration/summary 测试 |
| destructive 重建丢历史或 trigger | 三表逐列复制，恢复外键、索引和不可变 trigger         | schema 66→67 迁移测试  |
| 专题结果被当作候选               | required_suite=full；非 full 固定 SUITE_NOT_ELIGIBLE | API/Web/UI 测试        |
| 切换专题重放旧请求               | hook identity 含 suite                               | hook 测试              |
| 专题按钮引入远程调用             | 仍只接受 ready/healthy local loopback Provider       | 既有本地 Provider 门禁 |

## 明确不做

- 不开放自定义 suite、任意 case 勾选、case 排序或用户上传评测集；
- 不改变 dataset v3 的问题、知识片段或 expected facts；
- 不让专题套件形成 review candidate、发布许可或自动 Provider 路由；
- 不并行运行多个 suite，不调用远程 Provider；
- 不保存问题、知识正文、模型回答、reasoning、URL 或 key；
- 不实现 PDF/页码、句子级归因、自动调度或跨模型排名。

## 验收

- embedded dataset 校验四个专题各 6 case、单 category、中英各 3、完整覆盖和全局顺序；坏 key/重复/跨类/遗漏/乱序失败关闭；
- schema 66→67 迁移门禁、Run/Result/Review 历史逐列保留、专题 key 可写、未知 key 拒绝、外键/索引/trigger 恢复；
- Actor 至少运行一个专题 6/6，并验证 category 与语言分布；summary 将 smoke/topic/full 分组且专题固定不具备候选资格；
- 人工决定 API 可审计专题组，stale/幂等与无业务副作用保持不变；
- Web 严格解析六个 key，专题切换轮换幂等 key，设置页可启动四类之一并正确显示历史；
- 完整 Web、database、API race、`go vet`、gofmt、文档链接、桌面与 390 px 视觉门禁通过。
