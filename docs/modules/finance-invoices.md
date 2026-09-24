# 财务与发票模块

> 目标版本：v0.4（不并入 v0.1）。当前源码已交付本地账本与发票基础闭环，整体仍为部分完成；AI H3-E1 提供单次授权查询与工作台定位；H3-E2 另提供独立权限、逐项人工确认的本地收支创建/修改/作废，H3-E3 增加独立发票权限和草稿/状态/实际回款审批，H3-E4A 接入受额外确认的草稿及受控 PDF 删除，不执行外部付款。

## 定位与边界

H5-E30（2026-09-21）让本条显式 `workspace_ui+finance` 的智能体对已知真实 `financial_entry` 或 `invoice` ID 请求本地详情导航。Sidecar 核验读取范围和记录存在性，工具回执不带金额、备注、发票号、客户或 route；前端只从规范类型/ID 生成 `/income/:id` 或 `/invoices/:id`，用户点击确认后才打开并可返回对话。仅有 `workspace_ui`、普通 `work/actions` 不开放这两类导航；点击不自动读取详情给模型、生成 PDF、导出、标记付款或更改账本。见 [AI 精确记录导航](ai-assistant.md#智能体精确记录导航扩展h5-e302026-09-21)。

SQLite 保存用户录入或明确确认的事实。金额使用 `amount_minor` 整数，收入/支出由 `type` 区分，不用负数代替类型。不同币种不合并、不换算；`sent/viewed/paid` 是本地记录，不是投递、查看、银行到账或税务证据。

不接银行、支付、税务或外部催款服务；不执行付款、不自动发送发票。打开工作台详情只读取本地记录，不把人工可见的备注、客户名称或 PDF 隐式发送给模型。AI 本地账本提议要求独立 `finance_actions` 和人工财务确认；发票另需 `invoice_actions` 和人工发票确认，不从普通 `actions` 推断财务权限。

## 当前已实现

- schema 045：`financial_entries`、金额/币种/日期/状态/关联约束、版本与审计。收支创建、详情、编辑、带原因作废、分页筛选、CSV 导出和同币种统计已接前后端。
- schema 046：发票事实、唯一编号、草稿创建/编辑/确认删除、受控状态命令及付款原子入账。发票回款对应唯一账本记录，不可独立编辑或作废。
- schema 047：`invoice_pdf_assets` 和受控本地 PDF 生成、元数据、完整性检查与下载；备份恢复包含 PDF 集合。生成失败不改变发票业务状态。
- schema 048：发票临期、当天和逾期 Inbox 来源投影，稳定事件键去重；启动/运行期补偿只处理当前 occurrence，不补发每个错过的逾期日。逾期事件可驱动已启用的内置自动化，不对外催款。
- `/income` 展示实际月份/币种统计与明细、人工表单和作废确认；`/invoices` 提供真实筛选列表，`/invoices/:id` 提供详情、状态操作和 PDF。工作台右侧本月收入来自同一统计 API。
- H3-E1：独立 `finance` 授权提供 `workspace_finance`；新 `/income/:entryId` 精确详情和带日期/币种的报告地址与发票详情互通，支持返回原对话。收支与发票详情页可将精确记录身份交给智能体：只暂存本地路由和固定提示词，进入 `/ai` 后仍需用户显式授权 `finance+finance_actions` 或 `finance+invoice_actions`、手动发送并逐项确认。无本轮 schema 变更，当前源码基线 73。
- H3-E2：独立 `finance_actions`（须同时授予 finance）提供收支创建、修改、作废建议；完整本地审批、独立人工财务勾选、原生共享事务及结果往返已接通。并不等于开放所有财务写入，发票六命令由下述 H3-E3 独立接入。

以上基础财务功能不是 H3-E1 新建的；此轮校正旧文档中“页面骨架、schema 35、无 API”的过期状态。功能存在不代表全部 v0.4、真实模型或跨平台桌面验收完成。

## 用户流程与计算口径

1. **记收支**：人工选择收入/支出，填写金额、币种、发生日、分类及可选客户/项目/备注；保存后刷新账本和统计。金额正整数且不超过 9,000,000,000,000,000，币种为三位大写字母，分类最多 80 字、备注最多 10,000 字。
2. **作废**：人工二次确认并填写原因，携带观察到的版本；保留原记录与审计，不硬删除。已作废及发票付款生成记录不允许普通修改。
3. **建发票**：人工选择客户、可选项目、金额和日期，保存为自动编号草稿；只有草稿可编辑或删除。普通 PATCH 不能改变状态，付款通过专门命令。
4. **状态/PDF**：人工确认已发送、已查看或已付款；付款需日期。PDF 生成、查看与下载独立于状态，不表示外部发送。存储不可用、文件缺失/损坏、旧版本冲突均显示错误，不假报成功。
5. **付款与来源**：付款事务同时更新发票、唯一 confirmed 收入、事件及活动发票到期 Inbox 的解决事实，失败整体回滚；不把发票金额再次加入账本统计。关联 Task 不因付款自动完成。
6. **统计**：按账本 `occurred_on` 的已存自然日期、明确币种聚合；voided 排除，pending 与 confirmed 分列，净现金流只用 confirmed 收入减支出。平均收入为已确认收入整数除以记录数（向下取整），不是按客户计算的客单价；零记录显示零。不是银行对账报告。
7. **CSV**：人工导出当前筛选范围，接口要求 `confirm=true`；不存在 AI 自动导出或任意路径写入。

## AI 财务查询与往返（H3-E1）

- 输入区「工作台权限」→单独勾选「财务查询（只读）」→仅授权下一条消息。可用于不保存的会话，不依赖或自动带上 work/clients/actions。远程供应商会收到查询结果，停止只能阻止后续查询，不能撤回已发数据；Provider 版本变化、恢复中与取消沿用 Harness 门禁。
- `workspace_finance({view,...})`：`entries/entry/invoices/invoice/summary` 五种视图。明细/列表仅返回实际 ID、金额、币种、日期、状态、分类或发票号、版本、关联 ID、创建/更新时间及 route。SQL 不选择客户名称/联系字段、备注、作废原因、操作者审计、PDF 或附件；分类和编号仍是用户填写内容，可能包含敏感信息，不承诺自动脱敏。
- 列表每页 1–20（默认 10），offset 0–1000；可按币种、状态、客户/项目 ID 和日期过滤。账本另支持类型与精确分类，默认排除作废；发票 query 仅匹配编号的字面量子串，不搜索客户名称或备注。账本日期指 occurred_on，发票日期指 due_date。提供 has_more/next_offset/window_limited，不把有限窗口当全部历史。
- summary 必须显式给出大写三位币种与 date_from/date_to，范围 1–366 个含首尾自然日；不进行时区转换，不猜“本月”的用户时区。原生 `/stats/income` 与模型工具共用 `readIncomeStats`，保留上述计数/金额口径，不新增财务事实。
- 返回 `/income?currency=...&date_from=...&date_to=...`、`/income/<id>` 或 `/invoices/<id>`。主/侧对话严格限制链接；模型不能自填返回会话，UI 根据原消息添加 `return_session`。点击重新读取当前事实，不是冻结报告，也不是操作同意。
- 报告初次查询保持原币种/日期；期间锁定币种、隐藏月份控制，明确“指定期间”，用户可清除条件回到本机月份。无效范围不改查默认；刷新/失败不显示旧统计冒充最新。
- 账本详情核对返回 ID、刷新时隐藏旧记录、错误可重试、离开取消读取；作废/发票关联记录只读，普通记录显式点击才打开既有人工编辑表单。发票与付款账本可互相查看，保留原会话入口。详情页“交给智能体”只带入记录 ID 和 `/income/:id` 路由，提示模型先用 `workspace_finance(view=entry,id=...)` 读取元数据；如需修改或作废，再在 `finance_actions` 下起草待确认建议。人工详情仍可显示本地备注，不进入模型工具结果，也不会自动证明外部付款或银行到账。
- 工具结果只进入运行历史，模型可能引用到回答/会话记忆，遵循 persist。无额外正文审计、迁移或新财务 HTTP API；审批状态不作为付款证据。本地账本操作由下述 H3-E2 接续；发票草稿/状态由下述 H3-E3 接续；PDF 的 AI 生成及本次文件下载见 H3-E4B。

## AI 本地收支操作（H3-E2，源码与确定性测试已实现）

- **授权**：在工作台权限中选择「财务操作（独立确认）」，同时选中 `finance`；取消查询范围会撤销操作范围。只用于已保存会话，非法依赖或重复范围在创建 generation 前拒绝。普通 `work+actions` 即使叠加 finance 也不能写财务，finance+finance_actions 不授予任务/项目操作。下一条消息不继承授权。
- **提议**：`workspace_propose` 的 action 枚举按已授予的范围过滤；`financial_entry.create` 需要显式收入/支出、整数最小单位金额、币种、发生日、pending/confirmed 状态和分类，可附客户/项目 ID 与用户提供的备注。新建不能携带目标 ID/版本；update/void 必须使用查询得到的 `financial_entry_id` 和 `expected_version`，只允许各自字段；void 只接受 1–1000 字原因。不接受 invoice_id、操作者或模型提供的人工同意。
- **完整预览**：人工卡显示所有账本字段的前后值，包括完整备注、客户/项目名称和原生项目关联推导出的客户。金额同时显示币种、两位小数金额与原始最小单位整数，避免浮点舍入。备注 10,000 个 Unicode 字符上限与原生一致，财务动作输入最多 64 KiB、完整前后预览最多 128 KiB；不通过截断来获得可确认状态，整个模型请求仍受运行预算限制。
- **数据去向**：原有备注和关系名称只保存在本地不可变人工预览，不进入模型工具结果或后续审批回执。模型新提出的字段仍属于本次模型交互，若要替换备注必须由用户提供；查询工具没有隐式读取备注。审批表使用既有 schema 73，包含在 SQLite 备份，不进入便携业务导出。
- **确认**：三个动作都要求另行勾选，核对收支类型、金额、币种、日期、状态和关联，同意更新本地账本及统计，再由人工 decision 提交 `confirm_financial_effects:true`；缺失/false 返回 `422 FINANCIAL_ACTION_CONFIRMATION_REQUIRED`。拒绝或其他动作携带此字段返回 `422 AI_ACTION_DECISION_INVALID`。开始确认前必须生成完成且卡未过期，模型无法调用执行接口。
- **原子性与冲突**：人工账本 API 和审批共用 `financial_entry_commands.go`，确认时重验版本、状态、客户/项目资料及完整预览。发票回款生成记录和已作废记录不可修改/作废，关联变化或版本冲突需要重新读取并提议。业务记录、领域事件、审批决定与审批事件同事务；任何失败整体回滚，重复确认读取同一结果，不重复写账。作废保留历史、从统计排除，不退款、不硬删除。
- **回到工作台**：确认卡和后续无正文回执链接真实 `/income/<id>`；尚未确认的创建没有虚构详情地址。成功或模糊响应回读后，取消旧账本/统计查询并刷新列表、详情、本月收入与左侧收入角标。仅点击查看不授权额外写入，记录 confirmed 不是银行到账证明。
- **尚未接入**：账本权限不含发票；发票草稿/状态由 H3-E3 独立接续，草稿删除/PDF 由 H3-E4A/B 接续，CSV 导出由 H3-E5 独立接入，其他财务增强仍为后续工作；外部银行转账、支付、税务与自动催款不在本地账本能力内。真实模型质量与桌面验收单列，不以模拟模型测试替代。

## AI 发票操作（H3-E3，源码与确定性测试已实现）

- **独立权限**：选择「发票操作（独立确认）」同时启用 `finance`；取消财务查询会撤销依赖的写权限。`invoice_actions` 不继承或授予账本写权限、客户查询或普通 work/actions，仅保存会话可提议；未知/重复/缺少 finance 的范围在写 generation 前拒绝。普通 actions、finance_actions 或 finance 单独均不能提议发票命令。
- **六种命令**：`invoice.create/update/mark_sent/mark_viewed/mark_paid/mark_overdue`。创建明确 client_id、amount_minor、currency、issue_date、due_date，可附真实 project_id 与完整 notes；创建年份满足原生编号要求 2000–9999，只有确认后分配唯一编号。更新只允许草稿及上述字段；现有发票必须带实际 invoice_id/expected_version，禁止替换编号、自由写 status、操作者、跨领域 ID 或人工同意字段。
- **状态与日期**：mark_sent 仅 draft→sent，mark_viewed 仅 sent→viewed；mark_paid 仅 viewed/overdue→paid，必须明确用户实际收到全额回款的 paid_date，介于开票日和确认时本地今日之间。mark_overdue 仅 sent/viewed→overdue，必须已过本地到期日；其他状态动作 changes 为空。不从意向推断已发送、已查看或已到账，不调用外部服务。
- **完整人工预览**：展示编号、客户/项目 ID 与名称、整数金额/币种、全部日期、状态、完整备注的前后值；日期及金额必须与动作相符，客户端拒绝未知字段和隐藏变更。原备注与关系名称只在本地卡展示，不进入工具结果或后续回执；改备注需用户提供完整替换。notes 上限 10,000 Unicode 字符，动作输入/预览分别最多 64/128 KiB，超限拒绝而非截断，沿用 schema 73 和模型总预算。
- **逐项确认**：每种发票动作都需人工额外勾选 `confirm_invoice_effects:true`；付款明确核实全额回款及日期，逾期披露既有自动化的持续效果。缺失/false 返回 `422 INVOICE_ACTION_CONFIRMATION_REQUIRED`；拒绝或其他动作夹带该字段返回 `422 AI_ACTION_DECISION_INVALID`。生成未结束不可执行；确认时重算完整预览，目标版本变化返回 VERSION_CONFLICT，客户/项目资料变化返回 AI_ACTION_PREVIEW_CHANGED，均需新提议。
- **共享事务**：原生 API 与 AI 使用 `invoice_commands.go`，保留领域校验、唯一编号、事件及版本。确认时用同一个本地时钟校验日期，只有持久时间戳转 UTC。发票、回款的唯一 confirmed 收入、活动 invoice_due 来源的解决、领域事件与审批决定一同提交或回滚；不自动完成关联 Task，不再重复统计发票金额。重复确认只读原结果。
- **逾期自动化**：发票变更、逾期领域事件、当前已启用规则的投递捕获和审批同事务。提交后由既有投递器处理；已捕获事件不因后来停用而撤销。发票确认成功不证明自动化任务已成功创建，实际 Run 可失败并保留重试事实，须另查运行结果；本次 invoice_actions 不赋予模型启用/改写规则的能力。
- **回读与刷新**：确认或模糊失败后回读同一卡，取消旧发票/PDF 元数据、账本、收入、项目、Inbox 与搜索查询后刷新；逾期另刷新自动化、Task/Today 等读模型，防止迟到响应覆盖结果。卡片和下一轮无正文回执链接真实 `/invoices/:id`，继续支持原对话往返；权限不继承。发票详情页“交给智能体”只暂存发票 ID 和 `/invoices/:id` 路由，提示模型先用 `workspace_finance(view=invoice,id=...)` 读取元数据；如需改状态、删除草稿或生成 PDF，必须在 `invoice_actions` 下另起待确认建议。人工详情可显示客户名称、备注和 PDF 状态，但这些不会因点击交接而直接进入模型。
- **仍待**：CSV 导出由 H3-E5 接续，其他增强仍待；草稿删除、PDF 生成和本次文件下载已由下述 H3-E4A/B 接入，原生人工入口保留。真实模型效果、更新服务后的桌面联调仍需专项验收，不能用隔离测试代替。

## AI 草稿删除（H3-E4A）

- **权限与目标**：`invoice.delete` 复用独立 `finance+invoice_actions`，仅保存会话可提议；必须带实际 `invoice_id/expected_version`，`changes` 为空。只允许无账本关联的 draft，不取消已发送发票、不退款、不删除客户/项目。
- **人工核对**：完整草稿字段之外，预览包含可空的 `pdf_asset_id/pdf_file_name/pdf_size_bytes/pdf_sha256/pdf_generated_from_version/pdf_generated_at`；确认效果为 `invoice_deleted:true` 与 `pdf_removed`。这些是已存资产元数据，不证明磁盘完整性；没有路径或 PDF 正文，备注、名称及 PDF 元数据也不进入模型工具结果/后续回执。无资产时六字段全 null。
- **不可撤销同意**：除 `confirm_invoice_effects:true` 外，另需 `confirm_invoice_delete:true`，否则 `422 INVOICE_DELETE_CONFIRMATION_REQUIRED`；该同意不能由模型提供，也不能用于拒绝或其他命令。卡片明确永久删除草稿及其受控 PDF，无应用内撤销；审批预览和领域审计保留，不影响外部副本和既有备份。文件原已缺失时仍可清除资产记录，不宣称找回文件。
- **并发与补偿**：原生 DELETE 与审批共用 `invoice_delete_command.go`。按 PDF store→DB 顺序加锁，保持到提交/补偿完成；文件先移入受控 trash，发票/PDF 资产、领域事件与审批在同一事务中提交，失败将文件移回，成功后清理 trash。沿用原生启动 reconciler 处理崩溃或补偿失败；未变更备份、任意文件权限或 schema 73。已存 PDF 被添加/重生成、关联名称改变时，旧预览返回 `409 AI_ACTION_PREVIEW_CHANGED`；版本改变则 VERSION_CONFLICT。不会审批一个未展示的新文件。
- **回执与导航**：重复/并发确认只返回同一删除决定，不重复审计。`result_id/result_version` 表示已删除对象的 ID 和最后版本，不是仍存在的记录。确认卡与后续无正文回执链接 `/invoices`，不引导打开已删除详情；列表不新增返回参数，可经模式切换回原会话。模糊响应先回读，取消并刷新发票、PDF、财务及其他共享查询，不自动再次执行。
- **验证边界**：隔离数据测试覆盖有/无 PDF 的审批与数据库失败补偿、重复确认、添加/更换文件、目标/版本/客户变化、缺失/不安全文件、存储不可用、独立权限和两种人工同意。真实桌面/崩溃与模型专项仍单列；AI PDF 生成/本次文件下载由下述 H3-E4B 接续，未把删除包装成完整 PDF 能力。

## AI PDF 生成与本次文件下载（H3-E4B，源码已接通）

- **权限与预览**：`finance+invoice_actions` 在保存会话中可提议 `invoice.generate_pdf`，真实 `invoice_id/expected_version`，`changes:{}`。不限制草稿，沿用原生各状态生成契约；不改变发票版本、状态或账本。人工预览保存完整发票/备注、客户/项目名称及旧 PDF 六项稳定元数据；效果为 `pdf_generated:true/pdf_replaced:boolean`，没有伪造尚未生成的文件名、大小或哈希。确认重验全部预览，关联资料或 PDF 身份变化返回 `409 AI_ACTION_PREVIEW_CHANGED`。
- **独立同意**：确认既需 `confirm_invoice_effects:true`，也需 `confirm_invoice_pdf:true`；缺少后者返回 `422 INVOICE_PDF_CONFIRMATION_REQUIRED`，模型载荷、其他动作或拒绝携带该标记均不接受。确认卡明确在本机生成、旧 PDF 替换无应用内撤销；不外发、不自动下载、不确认付款。原备注、关联名称、PDF 路径/字节不进入工具结果或后续模型回执。
- **共享文件生命周期**：原生和审批复用 `invoice_pdf_command.go` 的受控 staging、提交、旧文件 trash 与事务后补偿。store→DB 锁保持到审批外层事务结束；新资产、历史结果及决定同事务。失败删除新文件并恢复旧文件，成功清理旧 trash；现有启动 reconciler 处理提交/回滚窗口的孤立文件，不把文件系统伪装为 SQLite 原子事务。恢复测试使用真实临时文件和数据库提交/回滚边界，不等同真实杀进程验收。
- **不可替换的历史结果**：确认后的 `invoice_pdf_result` 包含 `asset_id` 以及原生 PDF 元数据（invoice_id/file_name/mime_type/size_bytes/sha256/generated_from_version/generated_at/integrity_status/integrity_checked_at）。`result_id` 是实际 PDF 资产 ID，`result_version` 是来源发票版本；route 始终指向原发票详情。元数据存于现有 `idempotency_keys` 的内部 `AI invoice.generate_pdf` 命名空间，key 为 proposal ID，绑定 fingerprint，不能通过原生 HTTP 幂等键伪造；无 schema 变更。该记录随 SQLite 备份，排除便携业务导出；与原生幂等记录一样不是会话级正文清理对象。后续替换/删除不改写历史结果，缺失结果报错，不用当前资产补造。模型后续回执仅有操作、历史目标/结果 ID、版本与 route，不发送文件名/哈希或文件内容，不继承权限。
- **人工下载**：主/侧聊天确认卡展示生成时间、大小、来源版本和哈希，只有点击“下载本次 PDF”才请求 `GET /invoices/:id/pdf/download?asset_id=<UUID>&sha256=<hash>`。两个参数成对、唯一、严格校验；未知/重复/非法参数返回 `422 INVALID_INVOICE_PDF_LOCATION`。在 store 锁内先核对当前资产 ID/hash，替换返回 `409 INVOICE_PDF_CHANGED`；删除、缺失、损坏沿用原生 404/409，绝不静默下载新文件。无参数的人工详情下载仍保持原生行为。校验实际文件头、字节数和 SHA-256 后才响应；`X-Invoice-PDF-SHA256` 对允许的 Origin 暴露，客户端再核对哈希头及大小，关闭组件取消请求并忽略迟到结果。成功只报告交给浏览器下载，不承诺文件已保存。
- **验收与限制**：隔离 SQLite/受控文件、真实 Harness 配模拟上游覆盖读取→提议→双重人工确认→下轮无权限回执；另覆盖拒绝、预览冲突、审批失败补偿、并发重放、替换/删除后历史结果、精确下载和非法参数。真实模型、桌面下载保存、故障/跨平台仍单列待验收；CSV 导出由下述 H3-E5 接续，整体 H2–H5 仍未完成。

## AI CSV 导出（H3-E5，源码已接通）

- **独立权限**：`finance_exports` 依赖 `finance`，仅已保存会话可用；普通 `actions`、`finance_actions`、`invoice_actions` 均不授予导出权。模型只能提议 `finance.export_csv`，没有下载工具。接受消息后清除本次授权，Provider 身份、停止及恢复边界沿用 Harness。
- **显式筛选**：`changes` 仅允许 `export_filters`，无目标 ID/version/人工同意字段。必填大写 `currency`、`date_from/date_to`（1–366 个含首尾的实际发生日期）、`entry_type: all|income|expense`、`status: active|all|pending|confirmed|voided`。`active` 排除作废，`all` 包含作废；可选精确 `category/client_id/project_id`。不接受页码、任意排序、路径或模型提供的文件正文。缺少范围须询问，不能猜测。
- **完整结果**：与工作台共用筛选和 CSV 编码，导出全部匹配记录，不使用 AI 查询分页窗口；固定 `occurred_on DESC, created_at DESC, id ASC`。最多 10,000 条及 16 MiB，超限返回 `413 EXPORT_TOO_LARGE`，不交付截断文件；逐行读取/编码并限制缓冲区。空结果允许仅表头。原生人工导出沿用此行数/字节上限及编码防护，但保留原筛选、排序和 `confirm=true` 契约。
- **人工预览**：保存不可变筛选、全部匹配数、文件字节数、列、排序和 SHA-256；不把行正文存入审批或发送给模型。CSV 含 ID、type/status、amount_minor、currency、occurred_on、category、客户/项目名称、发票编号、完整 notes、created_at/updated_at。文本若可能被表格识别为公式（含前导空白/控制字符后的 `= + - @` 及对应全角字符）会前置单引号；不修改数据库原值，CSV 不是无损备份或可逆业务导入。
- **批准不是下载**：决策请求另需 `confirm_finance_export:true`，缺失返回 `422 FINANCE_EXPORT_CONFIRMATION_REQUIRED`；模型、拒绝或其他动作夹带同意均拒绝。沿用生成成功、24 小时、最多 8 项及决定幂等规则；确认事务重算预览，实际导出字节（含备注/关联名称）变化返回 `409 AI_ACTION_PREVIEW_CHANGED`。结果 ID 为 proposal ID、version 固定 1，表示审批身份，不是新增账本或文件资产；无业务详情 route，不刷新无关业务事实。
- **精确人工下载**：仅点击按钮请求 `GET /ai/actions/:id/export.csv?fingerprint=<64 位小写摘要>`，参数唯一、不接受覆盖筛选。非法参数 `422 AI_EXPORT_LOCATION_INVALID`；未批准 `409 AI_EXPORT_NOT_APPROVED`；审批不存在 `404 AI_ACTION_NOT_FOUND`；下载事务核对当前编码结果与原预览，变化返回 `409 AI_EXPORT_CHANGED`。以审批 ID 命名，返回 `Cache-Control:no-store` 和 `X-Financial-CSV-SHA256`；前端核对响应类型、摘要头及大小，离开卡片取消并忽略迟到结果。成功仅提示已交给浏览器，不承诺磁盘保存成功。
- **历史与隐私**：不持久保存 CSV 正文、不生成受控文件资产，无新 schema 或任意文件权限。历史卡表示原批准范围，数据变化须新提议，不能重放成新文件。审批按既有会话删除与 SQLite 备份策略保留/清理；外部已下载副本不受会话删除影响。下一轮模型仅收到 `approved_download_unverified` 与审批身份，不含筛选/计数/hash/正文，不声称下载成功或继承权限。此状态表示下载结果未验证，并非断言用户从未下载。
- **验证边界**：隔离 SQLite/模拟上游的真实 Harness 测试覆盖权限、临时会话拒绝、提议/确认/下载/无授权下轮回执；另测筛选、10,000 条完整性、字节预算、公式防护、数据及关联名称变化、审批回滚、幂等确认和非法参数。前端严格契约、独立同意、响应丢失回读及手动下载/取消纳入测试；真实模型和桌面保存仍待专项验收。

代码依据：[共享编码](../../services/sidecar/internal/api/financial_csv.go)、[审批与下载](../../services/sidecar/internal/api/ai_finance_export.go)、[后端测试](../../services/sidecar/internal/api/ai_finance_export_test.go)、[前端契约](../../apps/web/src/api/aiFinanceExport.ts)、[范围与下载卡](../../apps/web/src/components/AiFinanceExportDetails.tsx)。

## 数据、API 与事件

| 端点（前缀 /api/v1）                        | 当前能力                                                    |
| ------------------------------------------- | ----------------------------------------------------------- |
| GET / POST /financial-entries               | 分页筛选、新建收支                                          |
| GET / PATCH / DELETE /financial-entries/:id | 精确详情、版本化编辑、带原因作废（DELETE 需 confirm=true）  |
| GET /financial-entries/export.csv           | 人工确认导出筛选结果                                        |
| GET /stats/income                           | 指定币种/日期真实聚合；原生缺省仍为 CNY/当前月              |
| GET / POST /invoices                        | 列表筛选与新建草稿                                          |
| GET / PATCH / DELETE /invoices/:id          | 详情、草稿编辑、确认删除草稿                                |
| POST /invoices/:id/transition               | mark_sent / mark_viewed / mark_paid / mark_overdue 受控转换 |
| POST /invoices/:id/generate-pdf             | 本地 PDF 生成                                               |
| GET /invoices/:id/pdf                       | PDF 元数据与完整性状态                                      |
| GET /invoices/:id/pdf/download              | 完整性校验后下载                                            |

创建与状态命令支持 Idempotency-Key；编辑、删除/作废和状态命令按 If-Match 做版本校验，失败不接受模型或客户端覆盖事实。财务状态为 pending/confirmed/voided；发票为 draft/sent/viewed/paid/overdue。领域审计使用 `financial_entry_created/updated/voided`、`invoice_created/updated/deleted/sent/viewed/paid/overdue` 等下划线事件名。实际字段/命令以实现及测试为准，不新增 archived 状态或财务附件契约。

## 部分完成与后续规划

- **F1–F5 基础源码已实现**：事实迁移、账本、发票、PDF、基础统计与本地来源协作；不再标记未开始。
- **v0.4 后续增强**：MRR 规则、趋势图、真正按客户的客单价、项目完成后的开票预填、独立账本附件等尚未作为当前能力交付；外部银行/支付/税务/催款不在当前边界。
- **F6 验收仍需继续**：确定性测试覆盖金额/编号/并发版本、付款原子一致、PDF 完整性/备份与到期去重；真实桌面 PDF、跨平台、低磁盘及大数据量体验应单列验证，不能由模型 mock 或组件测试替代。
- **AI 后续**：H3-E2 已接独立账本操作权限与人工确认；H3-E3 已接发票草稿及四种状态命令。H3-E4A/B 已接草稿删除、受确认 PDF 生成及精确人工下载；AI CSV 导出已由 H3-E5 接入，不把这些切片当作全部财务操控。

## 验证与代码依据

- [账本与共享统计](../../services/sidecar/internal/api/financial_entries.go)、[原生账本测试](../../services/sidecar/internal/api/financial_entries_test.go)。
- [发票命令](../../services/sidecar/internal/api/invoices.go)、[付款等测试](../../services/sidecar/internal/api/invoices_test.go)、[到期来源](../../services/sidecar/internal/api/invoice_due_inbox.go)。
- [PDF 生成与下载](../../services/sidecar/internal/api/invoice_pdfs.go)、[PDF 备份测试](../../services/sidecar/internal/api/invoice_pdf_backup_integration_test.go)。
- [共享删除与文件补偿](../../services/sidecar/internal/api/invoice_delete_command.go)、[AI 删除事务/文件/权限测试](../../services/sidecar/internal/api/ai_invoice_delete_test.go)。
- [共享 PDF 文件事务](../../services/sidecar/internal/api/invoice_pdf_command.go)、[历史 PDF 结果](../../services/sidecar/internal/api/ai_invoice_pdf.go)、[审批/回滚/下载测试](../../services/sidecar/internal/api/ai_invoice_pdf_test.go)、[人工下载卡](../../apps/web/src/components/AiInvoicePdfResult.tsx)。
- [财务工具](../../services/sidecar/internal/api/ai_finance.go)、[授权/脱敏/分页/撤销/Harness 测试](../../services/sidecar/internal/api/ai_finance_test.go)。
- [共享账本命令](../../services/sidecar/internal/api/financial_entry_commands.go)、[AI 收支提议](../../services/sidecar/internal/api/ai_financial_actions.go)、[审批/回滚/长备注/Harness 测试](../../services/sidecar/internal/api/ai_financial_actions_test.go)。
- [共享发票命令](../../services/sidecar/internal/api/invoice_commands.go)、[AI 发票提议](../../services/sidecar/internal/api/ai_invoice_actions.go)、[六种动作/回滚/回款/本地日期/Harness 测试](../../services/sidecar/internal/api/ai_invoice_actions_test.go)、[严格前端契约](../../apps/web/src/api/aiInvoiceActions.test.ts)、[关联事实刷新测试](../../apps/web/src/api/invoiceActionFacts.test.ts)。
- [确认卡契约与精确金额](../../apps/web/src/api/aiFinancialActions.ts)、[确认交互](../../apps/web/src/components/AiWorkspaceActions.tsx)、[缓存取消与刷新测试](../../apps/web/src/api/financialActionFacts.test.ts)。
- [报告路由](../../apps/web/src/pages/IncomeRoutePage.tsx)、[收支详情与交接](../../apps/web/src/pages/FinancialEntryDetailPage.tsx)、[HTTP/Query/导航/交接测试](../../apps/web/src/pages/FinancialEntryDetailPage.test.tsx)、[发票详情与交接](../../apps/web/src/pages/InvoiceDetailPage.tsx)、[发票详情测试](../../apps/web/src/pages/InvoiceDetailPage.test.tsx)。
- [PRD](../opc-workspace-PRD.md)、[ADR-029](../adr/029-agent-workspace-capabilities.md)。
