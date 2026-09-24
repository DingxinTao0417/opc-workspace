# 桌面平台、可靠性与发布模块

## 单文件操作的本机结果待核对提醒（AH-04，2026-09-23）

单文件辅助流程的尝试记录与执行阶段日志（`workspace-file-attempts-v1`、prepared/started/outcome）只编译进独立 `file-security-helper` 进程，桌面主进程按 ADR-029 的进程边界**不读取**它们；此前界面上的“结果未确定，不要重复提交”只保存在内存里，应用在 helper 运行期间退出或重启后，用户没有任何提示去核对恢复记录。

现在四个可信主 WebView 写入入口（AI 项目文件修改、Agent 产出写入项目文件、版本 2 替换撤回/恢复、恢复预览写入）在调用原生命令**之前**写入一条本机提醒（`localStorage` 键 `opc-file-operation-reminders-v1`，最多 20 条），得到明确 `applied` 或用户 `cancelled` 回执后删除；`recovery_required`、`already_recorded`、回执异常或调用失败则保留为“需要核对恢复记录/结果未确认”。窗口重新启动时残留的 `in_flight` 一律转为“应用在操作进行中退出 · 结果未知”。提醒只含本地 UUID、来源类别、状态、时间戳和可选 AI 会话 UUID，严格恢复并丢弃任何多余字段；不含路径、文件名、内容、operation ID 或 helper 数据。

续办队列新增“文件操作待核对”分组并计入角标：点击打开右侧“文件”标签以核对“文件恢复”记录，用户确认后点“已核对恢复记录，移除提醒”手动清除。提醒明确标注“本机界面记录，非执行回执”，不证明成功或失败，不提供重试、恢复或写入入口，也不改变原生审查、UAC、helper、尝试占用或恢复记录。结果未知时仍禁止自动重试。

确定性证据：`fileOperationReminders.test.ts` 覆盖先记意图后结算、成功删除、未确认保留与手动移除、重启时 `in_flight` 转“结果未知”、畸形/含路径条目丢弃；`aiProjectFileReview.test.ts` 覆盖原生调用前已有意图、成功后清除、三种未确认结果保留且不自动重写；`AiSessionRail.test.tsx` 覆盖分组、角标、打开文件标签与移除。开发浏览器实测：写入 `in_flight` 提醒并刷新后续办队列显示“结果未知”、角标 +1，界面移除后存储清空。真实桌面 helper/UAC 流程中的提醒行为仍属 ACC-01 人工验收。代码证据：[提醒 store](../../apps/web/src/store/fileOperationReminders.ts)、[续办队列](../../apps/web/src/components/AiAgentInboxPanel.tsx)。

## 绑定发行构建的按次单文件操作入口（2026-09-22，部分实现）

可信主 WebView 现已接通受控产品入口，但只有**标准 Windows 桌面构建中存在与本次桌面二进制绑定的同目录辅助程序**时才动态开放。`workspace_file_write_capability()` 只读固定并重新哈希该辅助程序；普通 `cargo`/Vite 开发构建没有编译期摘要，保持只读，不接受运行时路径、摘要、环境变量或网页降级放行。模型、Sidecar、CLI 和内嵌浏览器子 WebView 均不能直接调用这些命令。

- AI 文件建议使用 `workspace_file_edit_apply(rootId,snapshotId,operationId,content)`；版本 2 恢复使用 `workspace_file_replacement_apply(rootId,reviewId,operationId)`。前者只能消费同一会话内未过期的精确原文件快照，后者只能消费用户刚复核的单次恢复票据；恢复的记录 ID 与方向从冻结请求派生，不采信页面另传。旧 `workspace_file_recovery_apply` 仍无条件拒绝，版本 1 原地写入不会被重新开启。
- `workspace_file_edit_apply` 现有两个可信主 WebView 发起者：本条主聊天的一份模型修改建议，以及用户从成功且已 `submitted` 的 Agent Run 明确选择的一份 file/files 产出。后者仍须用户在右侧“文件”中另选一个现有目标、建立完整且不超过 32 KiB 的精确基线并核对完整差异；Run 只提供候选正文，不提供本机路径或写入许可。两个入口共享同一原生审查/UAC/helper/恢复链，不扩大 Tauri 调用来源。
- 桌面进程全局只允许一个在途文件操作。规范 UUID operation ID 与当前 root ID 绑定；租约在原生确认、Windows 授权和管道等待期间持续复核目录选择、期限与取消标志。`workspace_file_operation_cancel` 只可取消完全匹配的在途操作，迟到取消幂等；它能阻止尚未发送的请求，但不能撤销辅助端已经写入 `started` 后发生的移动。
- 用户在网页完整差异上勾选只表示发起本次操作。随后独立 Win32 窗口再次显示精确方向和完整内容；只有用户继续，父端才以 `ShellExecuteExW(..., "runas")` 请求本次 UAC。父端固定已绑定 helper 映像及返回进程句柄，通过一次性命名管道传请求，并只接受绑定同一 operation ID 的规范终态回执。网页/命令行参数不携带本地路径、正文、解锁标志或任意命令。
- 明确取消原生窗口、辅助端窗口或 Windows UAC 会返回 `cancelled`，旧快照/票据已消费，UI 要求重新审查，不自动重试。除这些可证明的取消外，启动、传输、进程或回执错误一律按**结果未知**处理：刷新右侧恢复记录和文件事实，禁止重复提交。成功只表示 helper 完成目标/保留对象的完整回读并写入一次 outcome，不等于模型、Task、Artifact 或业务工作流成功。
- AI 建议写入会生成版本 2 恢复意图；恢复/撤销复用既有记录并保留原对象与候选对象。记录、阶段和恢复数据位于 app-local-data，含未加密路径及文件数据，不属于 Sidecar 数据库或业务备份；不自动迁移、删除或上传给 AI。当前无遗留记录清理 UI。

本轮确定性验证不弹真实 UAC、不运行已提权 helper、不修改系统审计策略，也不写用户项目文件。定向证据包括原生项目文件链 65 通过/1 个由父测试调用的夹具入口 ignored，及前端/API/store 64 项通过；完整 Rust/Web 门禁见本轮交付记录。真实已安装包的 UAC 交互、跨权限完整 SACL 保留、进程终止/断电恢复、签名发布与可访问性仍需人工专项，因此模块保持“部分实现”，不能描述为任意文件系统或自治编辑能力。

## 项目文件恢复原型与关闭的写入门禁（2026-09-22）

**本节记录已废弃的版本 1 原地写入原型，不再代表上节版本 2 产品入口。** Windows 实测发现持有 `share_mode(0)` 独占文件句柄仍可并发创建硬链接；在原文件上写字节可能影响用户所选目录外的别名。写前、写后检查链接数都不能消除两次检查之间的竞态。因此旧 `workspace_file_recovery_apply` 继续无条件拒绝，版本 1 UI 隐藏确认按钮；没有配置或用户确认可绕过。`workspace_file_edit_apply` 已改为消费精确快照并交给上节独立 helper 的无覆盖替换链，不再调用本节原地 writer。不能把下面的历史原型或测试写成现有执行实现。

- 已接通的可用部分：右侧文件面板的“文件恢复”入口，只读列举所选目录的实验性恢复记录，人工点击单项后核对当前文件与备份原文的完整差异。没有记录时不创建恢复目录；目录不可读、超过 64 项时明确失败，不显示截断列表；无效记录单独计数。刷新先清除旧列表，避免错误状态下把旧结果当最新记录。
- 原生命令 `workspace_file_recovery_list(rootId)` 返回 `records[{id,path,createdAt,restores,version}]/damaged/directory/replacementDirectory`，合并列出两个版本目录，每个目录最多 64 项。版本 1 的 `workspace_file_recovery_preview(rootId,recordId)` 返回 `id/snapshotId/path/currentBase64/originalBase64/expiresInSeconds`。沿用可信 `main` WebView 和用户选定 root ID，不向 Sidecar/模型开放。预览重验根目录和文件身份，身份替换拒绝；当前与原始字节不损失解码，非法 UTF-8/NUL 使用完整十六进制 diff。版本 1 快照同样最多八项、十分钟，切换、关闭、过期及迟到结果均释放；版本 2 使用下述独立只读观察，不签发恢复快照。
- 未开放的原型：独占句柄下比较基线 → 将原文、候选、路径和身份写入实验性 journal 并 `sync_all` → 原地写入/截断/刷盘/回读。重复 operation ID 仅返回 `already_recorded`，不再次写入；失败不自动重试。恢复预览冻结当时审查过的原文，不能在确认间隙偷换来源；恢复原型先备份当前字节再恢复，支持中断造成的非法 UTF-8。**原地写入策略存在上述硬链接缺口，只在隔离夹具测试，不可启用。**
- journal 位置为 `appLocalDataDir/workspace-file-recovery-v1/<operation UUID>.json`，版本 1，最多 64 项、单项读取上限 4 MiB；含精确原文/候选字节、绝对根路径、相对路径、身份、时间和可选来源记录。未加密、不自动删除、不属于 Sidecar 数据库或业务备份。SHA-256 只检测损坏，不是防恶意篡改认证；不要宣称原文不可篡改。当前正式入口不生成这些记录，只有测试夹具使用写原型。无 SQLite 迁移，schema079 不变。
- 后续已以独立 helper 的无覆盖对象替换链取代原地写入，见上节；本原型仍永久关闭。真实 Windows UI/UAC、断电恢复和真实 Provider 端到端仍待独立验收。

代码：`workspace_file_snapshot/windows.rs`、`workspace_file_snapshot/edits.rs`、`WorkspaceFileRecovery.tsx`、`projectFileWriteGate.ts`。原门禁切片的 21 项原生定向测试覆盖读取、备份/恢复原型和真实晚建硬链接行为；该阶段完整 Rust 门禁 69 通过、1 项既有 ConPTY 人工探针忽略。前端独立覆盖真实默认门禁，另行明确 mock 开启的测试只验证确认原型，不表示生产可写。临时夹具只操作测试专用目录并清理，没有写用户项目文件。

### 替换引擎的隔离验证进展

`workspace_file_snapshot/replacement.rs` 是仅在 `cfg(all(test,windows))` 编译的早期替代引擎，不进入正式命令或 UI。它验证了后来 helper started-only 执行器采用的关键约束：不取得原文件数据写权限，新建同目录候选 → 全文刷盘核验 → 持有已刷盘的版本 2 意图记录 → 用原文件句柄停放原对象 → 用候选句柄以 `ReplaceIfExists=false` 安装 → 核验新对象身份与全文。停放和安装均不覆盖已有名字，源文件从不被改写；因此晚建的目录外硬链接仍保留原文。API 语义依据 [FILE_RENAME_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_rename_info)，并由真实 Win32 夹具验证，不依赖原路径重新打开来选择要移动的源对象。

- 新文件在创建时传入原 owner/group/DACL/integrity label，写入正文前通过文件系统 `SetSecurityInfo` 保留 DACL 继承方式，再比较完整回读描述；不简单忽略发生变化的继承标记。已覆盖继承与 protected DACL 两种情况。此检查尚不含需特权的审计 SACL，不能宣称全量元数据兼容；额外数据流、扩展属性和普通属性的后续进展见下方。
- 冲突创建由新用户文件获胜，停放原文和候选都保留。安装前退出的模拟通过关闭所有句柄后重新读取实验意图完成：只允许把身份/全文仍一致的原对象重命名回**不存在**的原路径；原路径已有文件、父目录或原对象替换、原文漂移均拒绝。此恢复只移动同一对象，不写字节，所以允许后来出现的硬链接并保持其内容不变。它还不是实际进程强杀/断电测试。
- 新增 10 项替换定向测试通过；连同原基线/原型共 31 项，完整 `pnpm check:rust` 为 79 通过、1 项既有人工探针忽略。首次新文件创建因 Rust 创建选项缺失失败，随后修正；权限比较曾发现系统清除了 DACL 自动继承标记，改为显式保留后按原严格比较通过，没有放松断言。
- 下一步仍需完成审计安全元数据策略、已登记/崩溃遗留文件的容量管理及人工清理、逐次确认的写入/恢复集成及真实桌面验收。随后已接通的版本 2 只读列表，以及安装后撤销的隔离原型见下节；记录仍只由测试夹具生成，不能放入版本 1 目录或称为自动迁移。schema079 不变。

### 替换元数据与失败清理进展

元数据逻辑位于 `replacement_metadata.rs`；纯解码和原生只读捕获供记录/恢复条件审查使用，同一句柄约束的写回原语由独立辅助程序在候选实体化阶段使用。测试引擎与辅助端原语都通过持有句柄的 [BackupRead](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-backupread) / BackupWrite 保留有界 EA 扩展属性和命名 DATA 流（包含 `Zone.Identifier`），不把这些附带内容送给模型。主数据流必须与精确原文一致；流头、长度、名字和类型严格验证，合计最多 32 条、附带内容最多 64 KiB。不回放 BACKUP_LINK/security/reparse/未知流；链接始终属于保留的原对象。未知类型、截断、超量或特殊属性失败关闭，不以丢弃元数据的方式保存。

- 保留创建时间、读取前访问时间、hidden/system/not-content-indexed 属性；修改后的新文件设置 archive 标记，修改/变更时间由系统维护。只支持普通非只读文件；压缩、加密、稀疏、离线及重解析文件拒绝，不试图改掉标记后继续。
- 版本 2 测试意图增加原元数据及权限快照。停放前再核对主文、附带流和权限；缺失路径恢复也拒绝附带流已被改动的原对象。此比较仍不宣称通用 OS 沙箱或能抵御所有同用户/特权并发元数据操作；审计 SACL 策略尚未完成。
- 尚未持有完整刷盘 journal 的候选使用独立 RAII 句柄，请求在关闭时删除自己创建的那条链接，不按路径删除、不删除源对象或外部别名。备份失败/容量拒绝的正常错误路径已验证无候选残留；若原生删除请求失败或进程在析构前崩溃，仍需后续遗留检查，不把 best-effort 清理当成断电保证。journal 完整刷盘后候选不再自动清理，冲突双方留存。记录目录按所有条目限 64 项、单项意图不超过 4 MiB，枚举失败拒绝，绝不靠删除旧记录腾空间。
- 验证新增 13 项，替换引擎共 23 项通过，完整 `pnpm check:rust` 为 92 通过、1 项既有人工探针忽略。包含真实 NTFS ADS/Zone.Identifier、真实 EA、创建时间与属性、恢复元数据漂移、容量失败清理及只删除自有候选链接；解析异常与超限另有纯函数测试。测试临时目录已清理，未写用户文件；前端、Go、产品运行能力和数据库均未改，因此未重复其上一轮完整门禁。

### 版本 2 替换记录只读集成

右侧“文件恢复”可区分版本 1 原地写入实验记录与版本 2 替换意图。版本 2 独立读取 `appLocalDataDir/workspace-file-recovery-v2/<UUID>.json`，不存在时不创建；每个版本目录按全部条目限 64 项、单记录限 4 MiB，损坏/未知条目计数，枚举失败或超量整体报错。记录不互转、不迁移、不自动清理。版本 2 未保存创建时间与恢复来源，列表 `createdAt/restores` 为 null，不补造时间。绑定发行构建中的 AI 单文件替换可正式生成新记录；普通开发构建和旧版原地入口仍只读。

- 新增可信主 WebView 命令 `workspace_file_replacement_preview(rootId,recordId)`。重验记录、所选根目录和父目录身份后，只读观察目标、停放原对象及候选暂存；匹配已记录身份后才读取正文，不读取占位新用户文件的正文。已打开对象在观察结束前保持只读句柄；缺失位置无法跨路径加锁，结果只是逐项观察，不是原子成功回执或授权凭据。
- 返回 `id/path/observation/target/parked/staged/currentBase64/originalBase64/candidateBase64`。观察状态为 `source_present/target_missing/candidate_present/conflict`；各位置状态为 `missing/original/candidate/changed/foreign/unavailable`。缺失、其他对象或无法读取的目标 `currentBase64=null`，真正空文件为 `""`，不将两者混淆。占用/权限/不支持的对象不冒充缺失；已记录对象主文漂移显示 changed，正常晚建硬链接只读保留。
- 人工点击后优先显示拟恢复方向：`target_missing` 展示“缺失路径 → 记录原文”，说明只移回原对象、保留候选而不安装候选；原对象为空时明确恢复后是存在的 0 字节文件，不能把缺失当空文件。`candidate_present` 展示“当前候选 → 记录原文”，说明候选先移回空闲暂存，再移回原对象，保留双方、不覆盖新增文件。“记录原文 → 候选全文”完整转义文本/十六进制差异单独折叠为历史对照，不冒充本次恢复方向；其他可读当前对象仍可作只读比较，冲突不显示拟恢复计划。
- 初步观察不重验当前 ADS/EA/ACL，不证明全元数据一致、安装成功或可安全恢复；界面明确此限制。初步观察不签发快照，也不走旧版 apply；主动点击“复核恢复条件（只读）”才进入下面的有界审查票据。若绑定 helper 动态可用，票据核对完成后才显示恢复/撤销确认；否则保持只读。审查组件按 root ID/根路径/记录 ID/相对路径重新建立生命周期，切换立即卸载旧审查，迟到结果按原目录释放。关闭/切换/十分钟到期移除本地预览；刷新失败清除旧观察。
- 代码：`replacement_journal.rs`、`edits.rs`、`WorkspaceFileReplacementReview.tsx`。新增五项原生测试覆盖三个阶段、空文件/缺失、身份冲突不读正文、已知对象变化、父目录替换、占用、晚建别名和目录容量；新版界面九项测试及一个 IPC 测试覆盖完整预览、只读隔离、错误/过期/迟到清理。确定性测试不是原生 UI、真实模型、进程强杀或断电验收。

### 审查绑定的恢复与撤销原型

`replacement_recovery.rs` 中的捕获与有界票据用于正式审查；其 `freeze_request` 可由 `workspace_file_replacement_apply` 单次消费并交给独立 helper。普通权限进程中的历史移动原型仍只在测试编译，产品执行只发生在辅助端 started-only 内核。支持两种明确模式，不能根据磁盘变化自动切换：

- `MissingTarget`：目标必须不存在，停放原对象和候选暂存必须都与记录一致，只把原对象放回原路径。
- `UndoInstalled`：目标必须仍是未被修改的已记录候选对象，候选暂存位置必须不存在；先按候选句柄将它移回原暂存位置，再按原对象句柄放回目标。两次重命名均不覆盖已有路径，不改写任一对象的正文、属性或 ACL，不删除对象或其外部硬链接。后来手工修改过的候选不在此原样撤销范围内，不能自动丢弃。

审查冻结所选 root ID/绝对根、完整 Intent、两对象修改时间、有界 EA/ADS/普通属性与已支持权限描述；十分钟有效，不能 clone/持久化授权，任何提交尝试消耗本次审查。审查期间不留跨点击锁、不移动文件；执行前重新核对选择及根/父身份，以只读句柄固定 journal 并逐字段比较完整记录，再打开不带数据写权限的源对象句柄重验。内容、身份或已检查元数据漂移、来源记录被重算 checksum 后更换、过期和选择变化均拒绝。

复用原已刷盘 v2 意图中两份全文与身份，不另造成功标记或第二份字节备份：撤销在两次移动之间退出后，原文和候选仍分别位于记录的 original/candidate 位置，可重新进行 MissingTarget 审查。用户在间隙创建的目标文件获胜，双方保留，不自动回滚/删除/重试。原对象移回后的回读失败明确报告“已移回但核验失败”，不能谎称没有变更。原记录不是操作历史，恢复后 `source_present` 仍只代表当前观察，不证明从未执行或撤销事件已持久登记。

验证新增十项契约测试，覆盖原对象身份/下载来源流保留、审查零移动、重复恢复拒绝、模式/目录/期限绑定、正文/流/身份漂移、来源记录替换、目标/暂存竞争、晚建硬链接和句柄权限。另有一个默认门禁中的真实 Windows 子进程终止测试：父测试启动精确夹具子测试，确认它持有撤销间隙的文件/journal 句柄后，仅终止自己创建的进程句柄，wait 确认退出，再以全新审查恢复；无用户文件、数据库或服务参与。该测试确实终止进程，不再仅靠析构模拟，但仍不是断电、OS 崩溃、安装包或桌面 UI 验收。

完整 `pnpm check:rust` 为 108 通过、2 个顶层 ignored：一个是既有人工 ConPTY 探针，另一个是被上述父测试实际调用的子进程夹具入口，不是跳过恢复契约。所有临时目录与子进程已结束；前端/Go/API/记录格式/schema079 未改变，本轮不重复其前次完整检查。剩余：新候选的审计安全元数据策略、正式逐次审批桥接和内存票据容量、已登记/崩溃遗留的人工管理及真实端到端/断电专项；不得据此开放生产写入或宣称完整文件编辑已完成。

### 恢复条件审查票据与按次提权边界

版本 2 初步观察为 target_missing/candidate_present 时，人工可进一步点击“复核恢复条件（只读）”。新主 WebView 命令 `workspace_file_replacement_review(rootId,recordId,mode)` 接受 `missing_target/undo_installed`，独立重读记录与双方对象、验证完整主文、有界 EA/ADS/属性以及 owner/group/DACL/integrity label。成功返回初步预览字段加 `reviewId/mode/expiresInSeconds`；不改变路径或字节，不生成持久记录，不调用模型或启动提权。

- 本机内存最多八项审查，与普通文件快照独立；十分钟惰性过期，绑定 root ID、绝对根、记录、明确模式及精确文件事实。未知模式、超容量、元数据漂移或不支持文件拒绝。票据不是权限 grant，不发送给 Sidecar/Provider、不写历史；只允许上节产品命令在用户点击后单次消费。
- `workspace_file_replacement_review_release(rootId,reviewId)` 幂等释放，仅允许匹配的目录选择。UI 必须核对返回记录/路径/模式/正文与正在显示的完整差异及规范票据 ID/期限，不一致即释放；关闭、切换、刷新、替换旧审查、过期和迟到结果都释放。只有动态能力可用时才显示恢复确认；复核通过本身仍不等于原生确认、UAC 授权或执行成功。
- Windows 当前进程的只读权限检查确认没有 SeSecurityPrivilege。微软说明读取 SACL 需要打开时获得 ACCESS_SYSTEM_SECURITY，并由已有的 SE_SECURITY_NAME 权限支持；[GetKernelObjectSecurity](https://learn.microsoft.com/en-us/windows/win32/api/securitybaseapi/nf-securitybaseapi-getkernelobjectsecurity) 和 [BackupRead](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-backupread) 都不能通过普通读取绕过此边界。新增 `replacement_security.rs` 只抽出原有有界可读描述，不能把未读取的审计规则当成不存在。
- 用户已明确允许**设计并实现按次提权辅助程序**，边界是每次人工批准、仅所选单文件、不自动提权、不安装服务、不修改系统审计策略。独立可执行文件、权限底层、started-only 内核及受信产品启动入口已接通；真实已安装包中的跨权限 UAC/SACL、进程终止/断电验收仍待。本轮没有实际启动提权或调整 OS 令牌。不能让整个带网页/模型的主进程常驻提权，也不能接受任意 Shell、目录递归或可重放操作。

该阶段新增两项原生票据容量/选择/单次消费/过期测试（当时消费仍为隔离原型）、四项前端生命周期测试及一个 IPC 测试；原生完整门禁 110 通过、2 个既有顶层 ignored（其中夹具入口仍被进程测试实际调用）。后续产品消费入口见本页首节；“审查”本身仍不可对外宣称为授权或成功编辑。

### 单文件辅助程序：独立权限核心（部分实现）

已新增独立 Cargo binary `opc-file-security-helper`，现已迁入独立包 `apps/file-security-helper`，入口为其 `src/main.rs`，共用 `apps/desktop/src-tauri/src/file_security_helper/` 与协议/只读模块源码。它不加载桌面应用、WebView、Sidecar 或模型运行时；权限模块不由桌面 library 导入。`--status` 仍返回 `protocolVersion=1/status=approval_transport_internal_only/fileOperationsEnabled=false`，说明普通 CLI 不提供文件操作；`--verify-build` 只核验固定安装文件的签名配对。普通参数（包括路径、请求 JSON、任意管道及解锁参数）在项目文件操作前以退出码 2 拒绝，不回显敏感参数。唯一操作形状是受信桌面启动适配生成的六字段 `--review-v1` bootstrap；它不含路径或正文，须通过配对、父进程世代、管道、独立原生复核和完整阶段链，失败固定退出且不自动重试。父端 `runas` 适配现由本页首节的可信 Tauri 命令间接调用；没有真实安装/UAC 人工验收，不能称为通用提权编辑器。

- `privilege.rs` 已实现同步线程作用域：拒绝已有线程模拟身份；核验独立进程已提权后，只复制令牌、不调整进程令牌；在副本禁用已有 privilege 后仅启用 `SeSecurityPrivilege`，不启用 Backup/Restore/TakeOwnership。必须同时检查 API 成功与 last-error，`ERROR_NOT_ALL_ASSIGNED` 仍拒绝。线程作用域不可跨线程，退出恢复身份；无法恢复则终止辅助进程，避免带意外权限继续。默认入口和测试都不调用真实令牌调整。
- `security.rs` 使用 `BACKUP_SECURITY_INFORMATION` 请求完整描述符，包括 SACL，限制 20 字节至 64 KiB；任何不支持、拒绝、增长或结构无效均失败，不退回 DACL-only 或把无法读取当无审计。描述符只来自成功的内核查询，无 JSON/反序列化入口，不接受旧 v2 的部分安全描述冒充完整审查。[微软完整描述符定义](https://learn.microsoft.com/en-us/windows/win32/secauthz/security-information)与[权限调整成功条件](https://learn.microsoft.com/en-us/windows/win32/api/securitybaseapi/nf-securitybaseapi-adjusttokenprivileges)是此策略依据。
- 候选原语只用 `CREATE_NEW` 创建确定性 operation-bound sibling，在创建时携带完整描述，再分别保留 DACL/SACL 继承方式并完整回读逐字节核验。仅内部新建类型可应用权限，不提供改写原对象 ACL 的接口；无法设置原 owner/审计项时拒绝，不追加特权绕过。恢复意图绑定前，失败按候选自有句柄请求删除；记录和阶段绑定完成后，候选不可恢复为 disposable，正常错误/展开均保留它供人工核对。清理失败或进程崩溃仍可能遗留，不能把析构当持久恢复方案。
- **仍未验收的产品前提**：逐次人工审批通信、签名配对/父子进程身份、不可重放期限、可信产品发起层、root/operation 租约、受控执行、取消和规范回执已经串联；仍缺真实已安装环境的 UAC、跨权限 SACL、进程终止/断电恢复与签名发布专项。根/父目录固定、移动前身份重验、无覆盖执行和 unknown 语义不等于这些专项已通过。
- 验证：新增 8 项纯契约测试和 2 项普通权限真实进程入口测试；后者只调用状态/拒绝分支，不启动桌面或 UAC。完整 `pnpm check:rust` 为 120 项通过、2 项既有顶层 ignored。真正的特权获取、带 SACL/继承/不同 owner 文件的复制、完整主进程审批桥接及 Windows 原生交互仍未验收，不以 mock/常量入口检查替代。

构建检查已纳入原有 Rust 门禁，桌面 `Cargo.toml` 仍明确 `default-run=opc-workspace-desktop`，拆包不改变默认桌面启动目标。辅助权限原语沿用既有 windows crate；独立锁文件中的直接依赖版本与桌面一致，无数据库/业务 API 或恢复格式迁移。调试构建产物保留但不常驻，服务继续关闭。

### 独立辅助包与构建产物绑定（部分实现）

- `apps/file-security-helper/Cargo.toml` / `Cargo.lock` 单独构建辅助程序，直接依赖只有 base64、serde、serde_json、sha2、uuid 与 windows；没有 Tauri/WebView/HTTP/模型依赖。原辅助入口与两项真实 CLI 测试移入此包，共享协议/读取/审查/权限源码不复制，未删除或跳过测试。`pnpm check:tauri`、`test:rust`、`format:rust(:check)` 和 `check:rust` 覆盖两个包；影响分析也识别新目录。
- `scripts/build-desktop.mjs` 是 `pnpm build:desktop` / 桌面 `tauri:build` 的入口。Windows 先按 rustc 本机 triple、明确 target-dir 和 locked 依赖构建独立 helper；仅接受 `--debug` / `--no-bundle`，其他自定义 target/runner/config/features 拒绝，避免摘要与最终构建目标脱节。辅助产物先复制到独占新临时文件、校验摘要，再替换暂存目录项，不写穿既有硬链接/符号链接；失败回收自有临时文件，用户其他路径不清理。构建目录本身属于受信本地构建环境，不声称此脚本是 OS 沙箱。
- helper 的确定产物 SHA-256 通过本次子进程环境 `OPC_FILE_HELPER_SHA256` 进入桌面编译；脚本先清除继承值，不能复用调用者的旧摘要。不读写 `.env` 或全局环境。桌面侧 `option_env!` 只取编译值，运行时环境/argv/网页/模型不能提供替代摘要。临时 Tauri 配置将 helper 与既有 Sidecar 列入 `externalBin`，默认配置不改，所以直接 Cargo 检查/开发不会要求预先暂存 helper，也不会自动获得绑定。
- `file_helper_image.rs` 提供父端动态能力核验和执行期固定：固定当前桌面可执行文件所在目录及全部祖先，只打开同目录固定名 `opc-file-security-helper.exe`，不搜索 PATH、CWD 或其他目录；只读/共享读取句柄允许加载但拒绝写入/删除，拒绝重解析、目录、硬链接、空文件、超过 128 MiB 或摘要不符。保持原生身份/时间/摘要与目录句柄，在启动前、连接和回执边界重验；缺少编译绑定直接保持只读，没有自动信任或弹窗放行。
- 这是相对于受信桌面构建的**内容完整性绑定**，不是 Authenticode/发布者认证。返回进程到管道、父进程世代/同用户核对及产品启动已接通；签名/更新流水线仍必须在 helper 最终签名后再计算摘要，不能签名后继续沿用旧摘要。真实安装、发布者签名、UAC 和跨权限验证仍待。

验证：新增 5 项父端原生测试，包括在持有只读映像句柄时启动**自身测试可执行文件的 `--list`**，证明加载兼容但不执行测试操作/模型/UAC；另新增 3 项构建策略与暂存硬链接隔离测试。完整 Rust 门禁为主库 156、辅助 62、CLI 2，共 220 项通过，另 Node 构建测试 3 项通过；3 个顶层 ignored 的组成不变。`node scripts/build-desktop.mjs --debug --no-bundle` 实际通过，生成 `apps/desktop/src-tauri/target/<host-triple>/debug/opc-workspace-desktop.exe` 及同目录 helper；独立输出和暂存文件摘要一致。只构建、未启动桌面或制作/安装安装包；发行版签名/安装验收不能用此结果替代。

### 构建本地签名配对与只读自检（部分实现）

为避免“helper 摘要编入父端、父端摘要又需编入 helper”的循环，标准 Windows 构建现在使用一次性构建签名：在 Node 构建进程内生成 ECDSA P-256 密钥 → 只将公钥 X/Y 编入 helper → helper SHA-256 编入桌面 → 构建桌面且先不打包 → 签署最终两份映像的摘要 → 原生只读自检 → 按原选项打包。私钥 KeyObject 不导出、不写入磁盘、不传给 Cargo/其他子进程，签名尝试仅一次；不创建持久凭据或证书，也不承诺 OS 内存分页不可落盘。

- `scripts/desktop-build-binding.mjs` 定义固定域 `OPC_DESKTOP_BUILD_BINDING_V1\0` 加 helper/desktop 各 32 字节摘要的签名输入，使用 SHA-256 与 IEEE-P1363 `r||s` 签名；不对有歧义的 JSON 序列化字节签名。公钥通过本次 helper 编译环境 `OPC_DESKTOP_BINDING_PUBLIC_KEY` 传入，运行时仅用 `option_env!`；构建脚本清除继承的公钥与 helper 摘要，不接受运行时覆盖。
- 最终同目录的 `opc-desktop-build-binding.json` 只有 `version=1/helperSha256/desktopSha256/signature`，不含私钥、公钥替代项、用户内容、批准或权限。记录通过独占临时文件和目录项替换暂存，不写穿旧链接。Cargo 的顶层桌面程序可能与 deps 缓存共享硬链接；构建先独立复制待交付文件并替换其目录项，保持相同字节、不修改缓存对象，再签名。运行时仍拒绝硬链接。
- `file_security_helper/build_binding.rs` 通过 Windows CNG 验签，固定算法和嵌入公钥，记录上限 2 KiB、映像各 128 MiB；未知/重复字段、非规范编码、版本/签名/内容不符拒绝。固定自身目录及祖先、清单与两个程序的只读句柄，核验身份/时间/全文摘要，重验期间新增硬链接也拒绝。不搜索其他目录或用普通 hash-only 清单降级。
- `opc-file-security-helper.exe --verify-build` 可在已配对输出目录中普通权限运行，成功仅输出 `protocolVersion=1/buildPairVerified=true/fileOperationsEnabled=false`；失败退出 2、固定错误，不打印路径、摘要或输入。它只读取自身安装目录三个文件，不启动桌面/Sidecar、不提权、不连接管道、不读取项目。独立 Cargo 构建未嵌入公钥或缺少配对文件时失败。标准构建自动运行此自检，但不启动桌面。
- 内部 `InstalledPair::verify_parent` 将调用方的内核映像路径所指文件与签名确认的桌面文件/目录身份相比较，并已用于 helper `--review-v1` 入口；这不是已装载内存代码的测量，也不能单独排除既存进程启动后磁盘路径被换回合法文件。配对可信性以当前 helper/构建环境可信为前提，不是 Authenticode/发布者身份，不能抵御整套二进制被替换或受信进程注入，也不单独授予文件执行权限。
- Windows 默认分成 `tauri build --no-bundle`、签名配对/自检、`tauri bundle`，资源映射把清单放入安装目录根；`--no-bundle` 省略最后一步。实际安装资源位置尚待安装包验收。如果 bundle hook/正式签名在配对后改变程序，最终摘要检查使构建失败，已有失败产物不可交付；最终签名顺序仍需单独接入，没有安装或发布。非 Windows 流程不变。
- 验证：新增 6 项原生测试和 1 项普通 CLI 测试，包括 Node 公共测试向量由真实 CNG 验证、签名/字段/公钥篡改、安装文件缺失/修改/硬链接/晚建链接、无关调用进程拒绝和运行时公钥覆盖拒绝；Node 构建测试共 7 项，包括跨构建替换、一次性签名、清单别名保留与 Cargo 链接分离。完整 Rust 为桌面 179、辅助 68、CLI 3，共 250 项通过，4 个顶层 ignored 组成不变。实际 `node scripts/build-desktop.mjs --debug --no-bundle` 成功，配对自检为 true；首轮发现 Cargo 硬链接而失败，按上述产物分离修复，未放宽校验。未生成或安装安装包，没有真实 UAC/Provider 验收。

算法接口依据：[Node ECDSA 签名格式](https://nodejs.org/api/crypto.html#signsignprivatekey-outputencoding)、[BCryptVerifySignature](https://learn.microsoft.com/en-us/windows/win32/api/bcrypt/nf-bcrypt-bcryptverifysignature)、[ECC 公钥布局](https://learn.microsoft.com/en-us/windows/win32/api/bcrypt/ns-bcrypt-bcrypt_ecckey_blob)。新能力只增加已有 windows crate 的 Cryptography feature；无新 crate、业务 API、数据库/schema079 或恢复记录格式变化。

### 返回进程与映像固定到通信的绑定（内部实现）

`PinnedHelperImage::bind_process` 消费启动层交来的 `OwnedHandle`，不接受 PID、进程名或网页/模型提供的身份。`Peer::from_handle` 用不可继承、仅查询/同步权限的句柄保留同一个内核进程对象，取得创建时间、PID 与存活状态；不按 PID 重新打开。通过内核返回的映像路径，按相同的固定文件名、无重解析与完整摘要规则只读重验，要求目录和文件身份均与启动前固定对象一致。同名、同内容的另一个文件对象不能代替它。

- 可信调用层先固定映像，再由固定 `runas` 适配启动并直接交接返回句柄；不能传入查找得到的既存进程。产品入口已使用该顺序和辅助端认证/执行，但操作系统返回的路径仍不是内存代码度量，真实安装与提权验收仍不可由单元测试替代。
- `BoundHelperProcess` 将同一进程对象交给管道的内核对端校验，`BoundHelperConnection` 持有映像及祖先目录句柄直至一次请求/回执结束；接收连接前后、消息交换前后重验映像，进程存活/取消/期限仍由管道检查。任一失败消费当前对象，不自动重新启动、重连或重发，也不将未知通信结果解释为业务回滚。
- 当前已有绑定发行构建的产品调用和 UI 入口；映像核对仍不是发布者认证、提权令牌核验、防注入、父端认证或人工批准，必须与后续全部检查共同成立。没有业务 HTTP API、数据库迁移、恢复格式或新依赖变化。
- 新增 4 项桌面父测试实际运行自有测试二进制并管理退出，覆盖正确句柄到管道的回执、固定句柄生命周期、同内容异位置、已退出/非进程句柄、绑定后退出和晚建映像硬链接；不触发 UAC、不操作用户文件。原通信层 13 项测试现也在桌面目标运行。完整 `pnpm check:rust` 通过：桌面 173、辅助 62、CLI 2，共 237 项，另构建脚本 3 项；4 个顶层 ignored 中 3 个为父测试实际调用的夹具，1 个为既有 ConPTY 人工探针。真实跨权限/签名/原生人工交互仍未验收。

原生 API 依据：[DuplicateHandle 的同对象与权限语义](https://learn.microsoft.com/en-us/windows/win32/api/handleapi/nf-handleapi-duplicatehandle)、[QueryFullProcessImageNameW 的句柄与路径契约](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-queryfullprocessimagenamew)。

### 原生审查后的单次启动编排（绑定产品入口已启用）

`file_helper_image/launch.rs` 现在串联固定构建映像、完整原生审查、本次通信绑定、启动返回句柄与单次回执。`NativeReviewedRequest` 只能由原生窗口适配在生产代码中构造，不可反序列化、复制或由网页的 approved/普通审查核心决定直接构造；它仍不是辅助端执行授权。取消窗口直接结束；消费后沿用原请求与单调时钟期限，不重新签发有效期。

- 原生编排固定映像后才进入审查与启动，生产路径没有可替换的 launcher 参数。Windows 适配采用专用线程/COM apartment、固定可执行文件和安装目录、`ShellExecuteExW` 的 `runas` 与返回进程句柄；不拼接 cmd/PowerShell，不搜索路径、不展开环境变量、不启动服务、不重试。程序窗口请求隐藏，系统安全提示不绕过；确定性测试不会调用真实适配。
- 参数只由本次绑定生成：`--review-v1 <管道UUID> <父PID> <父进程创建时间> <随机值> <请求摘要>`，创建时间由父端内核句柄取得，不放项目路径、原文、候选或任意动作。所有字段为闭集规范格式；这些可观察元数据不是秘密凭据、完整父端认证或人工批准。helper 只对这一精确六字段形状进入内部接收，随后独立验证已签名安装配对、父进程世代/映像、同用户、内核管道对端与请求绑定；任一失败不自动重连、重发或降级。网页只能通过可信主 WebView 的窄命令发起，不能注册或直接提供该 bootstrap。
- 当前 root 选择和用户取消由桌面进程内单操作租约提供，回调短时、无副作用、可跨线程执行，不是网页传来的恒真标记。显示期间沿用原生窗口检查；启动/连接/发送/回执边界再次检查，等待期间另有名义 25 ms 的检查线程，一次发现失效便锁存并取消本次管道 I/O。正常、错误与展开退出都回收检查线程，不在后台继续等待。
- UAC 是系统控制的阻塞交互，应用不能保证即时关闭系统提示。父端人工审查链绑定原请求的剩余绝对期限，不重签十分钟有效期；普通连接/辅助端初次接收仍最多 30 秒。返回后失效不再发送正文。取消不是原子撤回已发送字节，也不证明已经 started 的辅助操作停止或回滚；因此除可证明的人工取消外，异常都按结果未知。原始响应必须经严格回执解析后才可作为本地文件终态，不是 Task 完成证明。
- 已有可信主 WebView 的 Tauri 产品入口和确认 UI；HTTP/CLI/模型没有操作入口。签名发布、真实 UAC/SACL、全局跨重启防重放及断电人工恢复仍未验收，旧版 v1 apply 继续无条件拒绝。
- 新增 6 项确定性测试，包括模拟原生交接、启动拒绝/取消不重试、审查后范围变化与到期不发送、等待期间取消，以及持有映像运行自有普通测试子进程并通过真实管道保持完整请求字节。实际 `runas` 不被测试调用，不能以测试代替真实 UAC/人工同意。完整 `pnpm check:rust` 通过：桌面 179、辅助 62、CLI 2，共 243 项，另构建脚本 3 项；4 个顶层 ignored 组成不变。只增加已有 windows crate 的 Shell feature，没有新增 crate、schema/API 或安装启动方式。

系统调用边界依据：[SHELLEXECUTEINFOW 的句柄、运行方式与安全提示约定](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/ns-shellapi-shellexecuteinfow)。

### 辅助端绑定请求与断连失效（内部实现）

`file_helper_transport/bootstrap.rs` 对六项内部启动元数据作有界解析：固定协议、规范 UUID、非零规范 u32 PID 与 u64 内核创建时间、各 32 字节小写十六进制随机值/摘要；未知/额外参数、前导零、溢出、非 Unicode 或过长参数拒绝。解析结果明确为 `UntrustedBootstrap`，不是批准凭据。父端内部启动适配已携带创建时间；该参数形状从未开放为产品 CLI，当前仍拒绝执行，未增加公共操作接口。

- `file_security_helper/parent_session.rs` 先取得已验签并持有的安装配对，按 PID 固定父进程句柄并核对创建时间，比较其映像路径对应文件与配对目录/桌面对象，再连接固定本机管道并由内核核对服务器 PID；接收期间持续保留同一进程句柄与配对文件。创建时间不符在连接前拒绝，不把可重用 PID 当同一进程。
- `ParentSession` 接收尝试先消费唯一连接，再重验配对、接收并校验绑定帧和严格单文件请求，解析后再次重验。失败不重连或重复接收；`ParentRequest` 借用配对对象，使用/回复前再验配对及连接。独立普通磁盘复核继续按只读流程执行，复核对象的生命周期同时保留配对；回执仍是未验证业务含义的字节，没有写入或批准语义。
- 已修复“父进程仍活着、但已关闭本次通道时，已接收请求仍被判有效”的缺口。`Incoming::check/reply` 现在通过异步管道句柄上的 `PeekNamedPipe` 检查连接及剩余数据，不消费额外字节；断连、不可用或额外请求数据即拒绝，不能等同于只检查进程存活。它不是对已发送内容或未来文件操作的原子撤销，仍须每个持久步骤重验及处理未知结果。
- 构建文件与 OS 对端绑定仍不是装载代码度量、不可伪造的人类在场或跨重启防重账本；前述磁盘路径换回合法文件/受信进程注入等限制仍适用。主 WebView 只通过上层单文件产品命令间接使用该内部层，CLI/模型不能调用；真实 UAC/SACL 和安装包验收仍待。没有新增 crate、业务 API/schema079、恢复格式或安装方式。
- 新增 4 项共享元数据/进程世代测试（桌面/辅助均运行）、5 项辅助端真实子进程组合测试与 1 项共享断连/额外数据回归。组合测试使用测试进程内临时 CNG 签名、复制的自有测试二进制和真实管道；私钥不导出或保存，未运行桌面/模型/UAC。覆盖正确接收、错误 PID/世代、错摘要/坏请求消费、晚建记录链接和父进程退出。测试夹具最初过早关闭 stdout 导致退出失败，已改为持续排空到进程退出；断连回归在生产修复前确实失败，修复后通过，没有跳过。完整 `pnpm check:rust` 为桌面 184、辅助 78、CLI 3，共 265 项及 Node 7 项；5 个顶层 ignored 为 4 个父测试实际调用的夹具与 1 个既有 ConPTY 人工探针。

连接观察的 API 边界见 [PeekNamedPipe](https://learn.microsoft.com/en-us/windows/win32/api/namedpipeapi/nf-namedpipeapi-peeknamedpipe)。真实跨权限、原生批准及安装包验收仍需单独执行，不能以组合夹具通过替代。

### 辅助端独立原生审查与原期限等待（内部实现）

`ParentRequest::native_review` 现在将已绑定接收的同一个 `CheckedRequest` 移入完整 Win32 审查，而不是重新解码或接受网页的批准标记。接受后核对请求全文摘要和原期限，再返回私有构造的 `ReviewedParentRequest`；该对象仍只支持未批准请求、普通只读磁盘复核与绑定回复，不是写入许可。安装配对及目录句柄贯穿窗口生命周期，窗口前后重验文件；250 ms 原生窗口回调只检查保留的同一父进程对象、取消事件、绝对通信期限及管道断连/额外数据，不在 UI 线程反复扫描映像。

- `ConnectionWatch` 仅持有原管道/进程的句柄副本及同一取消/期限，不携带正文、接收或回复能力。窗口返回后再次检查连接，父端关闭本次通道，即使进程仍活着或窗口返回了先前的“继续”，也不能交接有效请求。该观察不是并发文件操作的原子取消保证。
- 普通通道和辅助初次接收保持最多 30 秒；仅严格请求接收成功并开始原生审查后，辅助端才切换到请求剩余期限。父端 `Listener::for_request` 同样使用该请求的绝对期限，避免完整人工审查被固定 30 秒截断。`CheckedRequest::wait_deadline` 取原单调期限与声明剩余期限的较早值，不因移动、显示、等待或本机墙钟回退重签有效期；已超时的普通连接不能复活。原请求最长十分钟不变，父端原有范围/期限监听仍工作，辅助端不能靠自己的时钟延长父端等待。
- 人工取消只尝试发送绑定的固定 `{"version":1,"status":"review_cancelled"}`，不携带文件内容或“已回滚/已成功”状态；取消时期限或连接失效则返回错误，不制造收到取消回执的假象。错误交接、窗口失败或最终重验失败消费本次请求，不重试。
- 内部接线测试使用私有模拟渲染结果，现有 Win32 测试只创建隐藏控件；没有可见确认窗口、UAC、用户项目文件操作或安装包验收。真实跨权限人工批准仍待。CLI 继续拒绝操作参数；受信六字段 bootstrap 只可由绑定产品入口生成，确定性测试不调用真实启动适配。
- 验证：完整 `pnpm check:rust` 通过（桌面 187、辅助 82、CLI 3，共 272 项，另 Node 7 项），随后新增“父端在审查与交接之间断连”定向测试通过。覆盖原期限不续签、超过 30 秒的有界审查预算（检查绝对期限，不实际等待 30 秒）、普通超时不复活、观察不消费消息、断连/额外数据/取消/过期、精确交接与错误请求拒绝、取消回执及取消后拒绝继续。五个顶层 ignored 仍为四个被实际调用的普通子进程夹具和一个既有 ConPTY 人工探针；已有 `parse_ready_line` 未使用及链接器提示不属于本次行为变化。

没有新依赖、业务 HTTP API/schema079、恢复记录格式或安装命令；产品入口由本页首节补齐，模块仍为部分实现。

### 原生确认后的完整权限观察（内部接线，真实特权未验收）

`ReviewedParentRequest::capture_security` 将私有原生确认交接接入短时审计权限作用域。先重验配对与绑定请求，在辅助线程的独立令牌作用域内重新固定根/父目录和本次操作的原对象、可选已存在候选；先核对身份、完整正文/时间、普通元数据及可读权限，再在同一组句柄上捕获并回读完整安全描述。替换观察一个原对象，缺失恢复/撤销观察记录所指的双方；未核对身份的对象不进入完整权限捕获。

- 原普通读取句柄没有 `ACCESS_SYSTEM_SECURITY`，不能复用为完整 SACL 证明。仅增加固定的 `FILE_GENERIC_READ | ACCESS_SYSTEM_SECURITY` 打开方式，不申请正文写入、DELETE、WRITE_DAC 或 WRITE_OWNER。Windows 的 SACL 访问权同时允许读取和设置，并非 OS 层只读 ACL 权限；因此句柄仅留在内部捕获函数，没有文件句柄访问器，不进入返回值、网页或模型，实际回调只查询/比较。依据：[SACL 访问权](https://learn.microsoft.com/en-us/windows/win32/secauthz/sacl-access-right)与[句柄读取要求](https://learn.microsoft.com/en-us/windows/win32/api/securitybaseapi/nf-securitybaseapi-getkernelobjectsecurity)。
- `DiskReview::capture_security` 复用原路径/身份/内容复核规则，捕获与完整回读之间、回读后再验普通磁盘事实；缺失位置被新文件占用、替换原对象晚建硬链接、内容/普通权限漂移均失败。它不预留空名字，不声称安全描述和文件系统状态是原子快照。全部文件/目录句柄在离开审计作用域前关闭；成功、失败和可展开异常均释放，作用域再恢复线程身份。发布构建的 abort 异常由进程结束释放资源，不会继续带权限运行。
- `SecurityInspection` 只保留辅助进程内的完整描述和请求摘要，无反序列化、复制、序列化外发或执行许可接口。完成后再验配对、连接、原期限及请求摘要。完整捕获来自内核，不用模型字段或旧 v2 记录中的部分权限代替。观察后文件仍可能变化；未来持久执行必须再次核验，不能仅检查摘要就使用旧观察。
- 本切片当时没有候选创建、ACL 修改、正文写入、journal 持久化或实际提权调用。后续已补齐完整 SACL 新对象原语、尝试/阶段记录、候选实体化、started-only 执行、outcome 与绑定产品入口；CLI 仍拒绝操作参数。真实跨权限验收仍待。没有新增 crate、公共 HTTP API/schema079、恢复格式、安装方式或模型权限。
- 验证新增 5 项共享测试：三种操作的同对象捕获/回读、权限掩码、错误身份/元数据/权限在捕获前拒绝、捕获/回读/展开失败释放、目标竞争和晚建链接。使用真实隔离文件与普通权限，但注入捕获回调；未调整令牌或读取真实 SACL，不能当真实特权验证。权限作用域测试补充 String 错误释放与进入失败不执行。该轮 Rust 编译、Node 7 项、辅助 88 项及 CLI 3 项通过；桌面独立全套通过，但首次完整门禁曾出现进程测试失败，当时未归因。后续处理和最终门禁见下节，不能把当时重跑通过当作修复证据。

### 受信 bootstrap 到终态回执闭环（已接产品入口，真实 UAC/SACL 待验收）

`opc-file-security-helper` 现只对受信桌面启动适配生成的精确六字段 `--review-v1` bootstrap 开放内部入口。参数解析后先固定已验签安装配对，再按 PID 固定父进程内核对象并核对创建时间、同目录桌面映像身份和同用户 SID；随后连接唯一命名管道，由内核核对服务端进程并接收绑定随机值、请求摘要和完整正文的一次性帧。路径、正文、候选、动作名称和 `approved` 均不在 argv；普通路径/请求 JSON/任意管道/解锁参数继续拒绝，额外字段、非规范数字/UUID/摘要或缺少构建配对也不能进入审查。

- 辅助端接收后仍必须显示独立完整 Win32 审查；取消只返回绑定的规范 `review_cancelled`，接受后才进入 attempt、完整 SACL 观察、prepared/materialized/recovery/started、无覆盖移动及 outcome 链。Replace 会先实体化候选，恢复两种模式只使用已核验的记录对象。调用层不能提供批准布尔值、跳过完整权限检查或直接调用 executor。
- 完整成功后才编码 `{"version":1,"status":"succeeded","operationId":"<uuid>"}`；父端不再把任意响应字节当成功，而是要求小于 256 字节、无未知/重复字段、精确规范编码、固定版本/状态，并与本次 operation ID 完全一致。取消回执同样使用固定规范编码。绑定管道仍提供请求级关联；回执不能脱离该通道复用或当成新的操作授权。
- helper 对 bootstrap 的任何失败只输出固定错误、退出且不自动重试，不回显路径、摘要、父 PID 或内部阶段。失败可能发生在 `started` 前或后，父端不能仅凭进程退出推断“未修改”；必须按持久阶段和恢复记录核对，维持 unknown 语义。成功 outcome 已写但回执/连接失败也仍按未知展示，而不是自动重跑。
- 桌面内部 `review_and_exchange` 将固定映像、第一层原生审查、`runas`、返回进程句柄、管道和严格终态回执组合；现由上层可信 Tauri 单文件命令和网页人工按钮触发，仍无模型工具或自动触发点。CLI 的 `fileOperationsEnabled=false` 不代表绑定产品路径关闭。本轮确定性测试没有调用真实 `runas`、弹 UAC、安装服务、修改审计策略、读取真实 SACL 或移动用户项目文件。
- 新增规范终态回执测试在桌面与 helper 两个编译目标运行，并增加普通权限 CLI 进程测试，验证 malformed/额外 bootstrap 和缺少签名配对在项目读取前关闭。当前完整 `pnpm check:rust` 通过：桌面库 200 项、helper 单元 135 项、CLI 4 项，共 339 项通过、6 ignored；Node 构建绑定另有 7 项通过。真实 UAC/SACL、可见双层审查体验、安装包、强杀/断电各阶段和人工恢复仍需专项验收。

没有公共业务 API、schema079、依赖版本或恢复 v2 格式变化。可信桌面产品发起层和可见状态机现已接通；下一步是在真实安装构建上逐次验收，内部 argv 形状仍不得作为一般 CLI 能力公开。

### 辅助端完整准备与“结果未知”阶段（内部实现，未执行文件操作）

在上节最小尝试占用之后，`execution_journal.rs` 与 `execution.rs` 组成独立内部链路：原生完整审查 → 尝试占用 → 完整权限观察 → 写入 `prepared` → 绑定已核验候选为 `materialized` → 在固定 `workspace-file-recovery-v2` CREATE_NEW/刷盘/回读完整 intent → 写入 `recovery_intent` 绑定 → 新的同步 `AuditScope` 重验同一 live 对象的普通元数据、可读权限与完整 SACL → 重验原期限并最后写 `started` → 在 root/parent 继续固定时切换到重新核验的精确 DELETE 句柄 → 无覆盖移动 → 从仍持有的两侧句柄完整回读 → 写 `outcome`。新替换由完整观察产生确定性同目录候选，恢复类复用审查时已固定的候选和既有 intent。该 started-only executor 只接受 helper-bound 链；普通 CLI/HTTP/模型不能进入转换，网页只能经可信主 WebView 命令消费本机票据后间接发起。

- `<operation_id>.prepared.json` 冻结规范原请求、原对象与可选候选的完整普通元数据（创建时间、属性、有界 EA/ADS）及完整 owner/group/DACL/integrity/SACL 描述，并绑定 operation/review/request 摘要和准备时间。单条最大 3 MiB、最多 64 个操作，与 v1/v2 恢复记录和 metadata-only 尝试目录分开。内容使用规范 Base64/JSON、有界严格解码和 checksum，但未加密，可能包含所选文件原文、候选全文、绝对根路径、附带流和权限；不进入业务数据库备份、模型或聊天历史。
- 捕获不再只保留 SACL：在相同已固定、已验证身份的只读句柄上同时读取普通元数据和完整安全描述，回读安全描述并重新比较稳定元数据；随后磁盘层再次核对正文、身份、时间、可读权限和空闲目标。进入尝试目录前的短期授权不会因持久解码而复活；新增 `validate_persisted` 只检查旧请求结构/原期限边界，不产生新的 `CheckedRequest`。
- `<operation_id>.materialized.json` 只存身份、时间、候选名称与各类摘要，不重复存正文或权限描述。新替换只接受 `.opc-file-<operation_id>.candidate`：必须是同卷新身份，正文与请求完全一致，普通元数据符合原对象的候选语义，可读权限与完整权限均与本次观察一致，且核验期间没有变化。恢复/撤销的候选身份、时间、正文、元数据与权限则全部来自同一次完整观察。候选只能绑定一次，无 materialized 时 `mark_started` 必定拒绝。
- 新替换的内部实体化只在新的同步 `AuditScope` 内打开源对象的完整权限查询句柄，句柄无数据写入/DELETE/WRITE_DAC/WRITE_OWNER；然后以 CREATE_NEW 创建确定性候选，写入完整正文，通过句柄回放有界 EA/ADS/属性与 owner/group/DACL/integrity/SACL，再前后核验源和候选的身份、链接数、正文、元数据及完整权限。候选对象持有源文件与 root/parent 固定句柄；recovery 绑定前失败或释放，只对自己创建的候选句柄请求删除，不按路径删除用户文件。绑定后先不可逆地切换为 retain，再允许写 started；此后任何错误都保留候选。实体化/保留转换均为内部私有组合，没有 CLI/IPC/Tauri 调用者。
- 固定恢复目录的新建路径只接受与冻结请求、完整观察和实体候选完全一致的 v2 intent；每条以既有 `{"intent":...,"digest":...}` 格式 CREATE_NEW、`sync_all`、逐字节回读及严格解码，容量仍为 64。记录句柄和目录持续固定。恢复/撤销则打开、严格核验并持有既有记录；两条路径都不信任调用者提供的 store 路径，也不修改项目对象。
- `<operation_id>.recovery.json` 不重复正文，只把同一 operation/review/request/prepared/materialized 与恢复记录版本、ID、intent 摘要、存储字节摘要、记录文件身份/时间、源身份和候选身份绑定。新替换的记录 ID 必须等于 operation ID，记录候选身份必须等于 materialized 身份；恢复/撤销还必须匹配请求内的记录身份与摘要。无 recovery 绑定时 `mark_started` 必定拒绝；绑定只能 CREATE_NEW 一次。
- `<operation_id>.started.json` 只含同一操作/审查/请求/准备/候选实体/恢复意图摘要与开始时间，用 CREATE_NEW、刷盘、回读生成。只能从仍在原期限内、持续持有 `prepared`/materialized/恢复记录、recovery 已绑定，并且刚在新的短时审计作用域重验完整 SACL 的内部对象转换，不能直接从 JSON 构造。该复核不写文件、不延长审批期限，任何身份、正文、元数据、可读权限或完整权限变化都拒绝 started。它是首个移动前最后一个持久动作；started-only executor 随后才交换 DELETE 句柄。只要 started 存在但没有有效 outcome，重启扫描就将其标为 `UnknownAfterStart`，重复注册明确返回“结果未知”，不得自动重试、假称失败或重新使用旧审查。
- `<operation_id>.outcome.json` 只表示完整核验后的成功，不提供可伪装成“安全失败”的通用错误回执。它绑定 operation/review/request/prepared/materialized/recovery/started 全链摘要、完成时间、操作模式，以及目标对象和保留对象的身份、正文、普通元数据、可读权限与完整权限摘要；所有值必须等于冻结请求及阶段链导出的唯一终态。回执 CREATE_NEW、刷盘、回读且只能写一次。执行开始后的批准期限可以自然到期而不阻止记录已发生事实，但不会因此授权任何新移动；不匹配、损坏、缺失或写入失败都保持 `UnknownAfterStart`。
- `execution.rs` 只接收内存中完整观察和已 started 的私有对象。Replace 先把原对象移到 `.opc-file-<operation_id>.original`，再把新候选移到目标；RestoreMissing 只把记录中的 original 移回空目标；UndoInstalled 先把当前候选移回记录中的 candidate 名，再把 original 移回目标。所有移动都对持有句柄调用 `FILE_RENAME_INFO` 且 `ReplaceIfExists=false`，不按路径删除、覆盖或截断；root/parent 始终由已核验句柄固定。移动句柄只有读、DELETE 与完整 SACL 查询权，没有正文写入、WRITE_DAC 或 WRITE_OWNER。句柄交换后、首个移动前再次核对身份/正文/时间/普通元数据/可读权限/完整 SACL；竞争对象占位则拒绝并保留现场。首个移动后的失败不自动回滚，仍由 started 无 outcome 表达未知。
- 每次新尝试在写入前完整扫描阶段目录：prepared 必须对应已登记 attempt，materialized 必须对应 prepared 且完整验证候选绑定，recovery 必须对应 prepared/materialized 与请求中的恢复事实，started 必须同时对应前三者摘要，outcome 必须再对应 started 及唯一预期终态；未知文件、孤儿、重复审查、损坏/扩展/非规范编码、容量超限均失败关闭且不修复。固定句柄贯穿会话；既有项不以写权限打开，新阶段 CREATE_NEW 不覆盖。创建任一阶段后发生写入、刷盘或回读错误会保留空/截断项，后续人工处理。扫描最多允许 64 个操作的五类记录，prepared 仍是每个操作的必要根记录。
- 边界：checksum 只检损，不认证同用户历史；严格 thaw 只证明存储的安全描述语法有效，不能授权把可改写的磁盘 ACL 应用到文件。同进程执行只使用仍在内存中并重新核对的内核捕获；重启恢复必须把持久描述与 live 对象实况重新比对，不能只信 stage 文件。产品调用者和可见审批入口已接通，但真实提权、跨权限 SACL、安装包、强杀/断电全阶段、遗留自动清理或持久密钥仍未验收；不能据此宣称任意文件编辑已经完成。安全语义不是“执行失败零副作用”，而是“首个移动前失败不改项目对象；首个移动后失败保留可恢复证据并明确 unknown”。
- 设计对应 Codex 的两个边界：审批先于运行，且开始/结束事件用同一调用身份关联；批准状态不能在重试时丢失，执行开始也不能冒充完成。参考 [Codex 协议中的审批与执行事件](https://github.com/openai/codex/blob/main/codex-rs/docs/protocol_v1.md) 与 [ToolOrchestrator 的 approval → sandbox → run 顺序](https://github.com/openai/codex/blob/main/codex-rs/core/src/tools/orchestrator.rs)。本项目没有照搬 Shell、会话级通配授权或自动无沙箱重试。
- 阶段与恢复记录测试覆盖完整冻结、prepared/materialized/recovery/started/outcome 重开分类、新候选精确单次绑定、恢复候选自动绑定、缺少实体或恢复意图时拒绝开始、过期后只读识别但不能开始、started 后到期仍可记载已核验成功、重复终态、孤儿/未知/各阶段篡改、容量和零创建；另验证新 intent 只创建一次、刷盘回读、持有期间不可改写，不匹配 intent 不创建 store 或项目写入。本轮再增加候选在恢复绑定前删除/绑定后保留/句柄移交，执行句柄不含数据/ACL 写权，以及 Replace/RestoreMissing/UndoInstalled 的真实临时文件布局、无覆盖移动和竞争者不被覆盖。最终 `pnpm check:rust` 通过：桌面 198、辅助 134、CLI 3，共 335 项 Rust 测试通过，另有 6 项顶层 ignored；Node 构建绑定测试 7 项通过。确定性门禁没有 UAC、令牌调整、真实 SACL 捕获、产品项目文件移动或桌面验收。

无公共 API、schema079、依赖版本、启动命令或恢复 v2 格式变化。首次真实内部准备才创建 stage 目录；不迁移或自动清理，模块保持部分实现。

### 辅助端持久尝试占用（内部实现，非执行回执）

`attempt_journal.rs` 增加跨进程的保守重复尝试门禁。内部 `ReviewedParentRequest::capture_security` 在原生完整审查、配对/连接验证和来源恢复记录检查后，先登记本次操作，再重验活连接/原期限/记录，才进入审计权限观察。`RegisteredSecurityInspection` 私有持有本次记录、旧记录和存储锁直至观察对象释放；只有绑定产品入口可间接到达，不能由 CLI/模型公开调用。

- 固定位置 `appLocalDataDir/workspace-file-attempts-v1/<operation_id>.json` 仅从同用户 `RecoveryLocation` 推导。要求已有应用目录、固定祖先，只创建尝试目录这一层，不创建 profile/应用祖先，不接受外部路径，不改恢复 v1/v2。`store.lock` 是零字节普通文件，新建或只读独占打开；拒绝硬链接、重解析及非空内容，另一实例占用立即失败，不轮询。依据：[CreateFileW 创建与共享语义](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)。
- 记录严格包含 `version=1/operation_id/review_id/request_sha256/registered_at_ms`，无路径、正文、ACL 或凭据。单条最多 1 KiB，共最多 64 条加锁文件；登记前有界完整检查并固定旧项，未知、损坏、重复历史审查、超限或读取失败均拒绝。操作 ID 或审查 ID 任一已登记，即使正文或另一 ID 变化也不能重试，不清理腾空间。
- 新记录仅 CREATE_NEW 写入、`sync_all` 并逐字节回读，不重写已有对象。创建后任何失败保留原条目，包括空/截断数据；析构、观察失败、取消或进程退出不撤销占用。仅表示“尝试已占用”，不证明移动开始、成功、失败零副作用或可以续跑。完整恢复意图、阶段回执和私有 started-only 执行后来已补齐；**本阶段记录形成时**产品调用与人工管理界面仍未实现，当前受限产品入口见本页首节。
- 存储可被同用户删除/改写，没有签名、持久密钥或防篡改 ACL，不能作为抵御恶意同用户的防重放证明。刷盘回读与进程强杀测试不等于掉电、OS 崩溃或目录项持久性保证；[Rust sync_all 约定](https://doc.rust-lang.org/std/fs/struct.File.html#method.sync_all)。产品入口已接通，但本轮确定性验证不修改系统策略、不实际启动提权。
- 新增 10 项测试覆盖身份消费、最小元数据、并发、空/截断/未知/重复字段、严格版本/摘要、历史身份重复、容量、过期和硬链接。父测试实际启动普通子进程，登记刷盘后强杀，重新打开目录验证旧请求拒绝而新请求可取得已释放锁。最终 Rust 门禁为桌面 194、辅助 106、CLI 3，共 303 项，另 Node 7 项；六个顶层 ignored 中五个为实际调用夹具，另一个是既有 ConPTY 人工探针。无真实 UAC/SACL、用户 profile/项目写入或断电验收。

无新依赖、公共 API/schema079 或恢复格式迁移。内部首次尝试将建立独立目录，不自动迁移/清理；**本阶段记录形成时**产品入口仍未接通，后续已接通的入口与当前剩余验收见本页首节。

### 辅助端固定目录的恢复记录复核（内部实现）

恢复请求的完整权限观察前，辅助端独立定位并持有 v2 记录，不再仅相信请求中的 `record_sha256` 声明。新替换请求没有来源记录，不打开旧记录；缺失恢复/撤销必须通过以下检查才能进入审计权限作用域。CLI 仍关闭；绑定产品入口可在消费精确票据后发起正式恢复执行。

- `ParentSession` 在构建配对/进程世代验证后，读取父进程与自身的内核 TokenUser SID 并要求一致；不比较用户名、环境变量或网页字段。定位前后拒绝线程已有模拟身份，不设置/替换其令牌。仅支持同账户的辅助流程，使用另一管理员账户的跨用户提权不被当作同一恢复存储授权；真实跨权限场景仍未验收。
- `RecoveryLocation` 用系统 `FOLDERID_LocalAppData` 和固定 `com.opcworkspace.desktop/workspace-file-recovery-v2` 定位，匹配桌面 app-local-data 约定；不接受 argv/JSON/环境路径覆盖，不创建目录或跨用户查找。Shell 分配的路径在成功或失败时均释放。读取时仍逐级固定本机目录、拒绝重解析/网络路径，精确打开 `<record_id>.json` 并拒绝硬链接/写入占用，单记录最多 4 MiB。路径映射依据：[Windows Known Folder](https://learn.microsoft.com/en-us/windows/win32/api/shlobj_core/nf-shlobj_core-shgetknownfolderpath)、[Tauri 应用本地数据目录](https://docs.rs/tauri/latest/tauri/path/struct.PathResolver.html#method.app_local_data_dir)。
- `file_recovery_record.rs` 抽出桌面/辅助共用的严格 v2 解码和规范摘要，保留原字段顺序、序列化内容与磁盘格式；未知/重复字段、坏摘要、错误版本、超限或非法路径失败。辅助端逐项比较请求的记录 ID/版本/摘要、根/相对路径、根和父目录身份、原/候选身份与完整正文，以及原元数据、候选 NORMAL→ARCHIVE 规则和双方可读权限指纹。访问时间不纳入元数据指纹，原始记录摘要仍绑定整个规范记录。实际目标/暂存位置、当前修改时间、文件状态及完整 SACL 继续由独立磁盘/安全检查证明，不以记录替代。
- `HeldRecoveryRecord` 持有同一记录和祖先目录，记录精确字节摘要、对象身份和修改时间；完整权限观察前后复核，晚建记录硬链接或变化拒绝。记录 I/O 后再次核对配对、连接与原期限，才进入审计作用域；普通记录句柄贯穿观察及退出后的检查，审计访问文件句柄仍先于权限恢复释放。失败不重试、重写、迁移或清理记录。
- v2 的 checksum 和请求绑定不是认证签名、不可篡改历史或跨重启防重放账本；检查只能证明当前记录与本次冻结审查一致。记录中的部分 ACL 永不送入 setter，也不冒充完整 SACL。没有创建新的持久意图/回执、执行移动或开放恢复；持久执行协议与真实人工/UAC/SACL/断电验收仍待。
- 验证：6 项辅助端临时记录测试覆盖两类恢复、只读持有、13 类请求不符、损坏/重复字段/超限、连同 checksum 一起改写、缺失目录零创建、新替换零读取与晚建链接；新增共享同用户 SID 判断及旧 v2 格式金样，补候选属性指纹与访问时间检查。最终 `pnpm check:rust` 通过：桌面 194、辅助 96、CLI 3，共 293 项，Node 7 项。所有项目文件读写仅在测试专用目录；真实 profile 测试只查询系统目录位置和 SID，不读取其中记录，不调整令牌或触发 UAC。

仅扩展既有 windows crate 的 Shell/Com 功能用于系统路径和内存释放，无新 crate 版本、公共 API、schema079、恢复格式或启动/安装命令。模块保持部分实现。

### 进程生命周期与请求期限的回归校正（仅测试）

已复现此前 `returned_handle_binds_exact_image_and_keeps_pins_through_pipe_exchange` 的失败：消息交换与退出检查成功，随后重命名刚运行过程序的临时目录时收到 Windows 错误 5。先显式释放父侧 `Child` 句柄后仍能复现；同一次失败中，目录 DELETE 访问探针已成功，证明应用的禁止删除共享锁已经释放。不能把该即时目录树重命名结果等同于应用锁生命周期；具体剩余外部条件未取证，不归因于某个杀毒程序或系统服务。

- 进程集成测试改为直接验证同一个目录的 DELETE 访问：绑定期间拒绝，单次请求结束后即使自有子进程尚活着也允许。探针只打开/关闭句柄，不设置删除标记或执行删除；未运行可执行文件的独立夹具仍验证真正的目录重命名在释放后成功。原消息、对端身份、持有期间拒写/拒移动、过期与取消契约均保留，不增加睡眠重试或跳过。
- `ChildFixture::finish` 等待自有进程退出后消费 `Child`、关闭父侧句柄；等待期间仍保留所有权，异常 Drop 只清理自己创建的进程。终态可断言无剩余 Child，不把观察到退出等同已释放所有父侧资源。[进程退出与最后句柄的区分](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-exitprocess)也支持这个资源边界，但不单独证明上述目录拒绝的具体来源。
- 最终门禁还暴露独立的期限测试误差：两次 `wait_deadline(now)` 使用毫秒墙钟采样和更细的单调时钟，其估算值不保证彼此严格递减。改用仅 `cfg(test)` 的观察入口，直接断言交接前后保留的原始 `Instant` **精确相等**，比估算大小比较更直接验证未重新解码/续签；生产期限逻辑不变。
- 校正目录断言后 8 轮桌面全套通过，最终代码 `pnpm check:rust` 通过：桌面 192、辅助 88、CLI 3，共 283 项，另 Node 7 项。五个顶层 ignored 组成仍是四个实际调用夹具和一个既有 ConPTY 人工探针。没有新生产行为、权限、入口、协议/持久格式或依赖，也没有实际提权、SACL 或真实人工批准验收。

### 辅助通信通道（产品链内使用，仍需真实跨权限验收）

`src/file_helper_transport/` 实现父进程与独立助手的本机通道，由辅助 binary 和桌面 library 共同编译。它通过受信六字段 bootstrap 接到辅助端完整流程，并由可信主 WebView 的上层单文件命令间接使用；确定性测试不触发 UAC 或用户项目文件操作。通道本身只传递有界字节，不单独证明单文件范围、人工批准、已审查差异或程序签名；完整授权由配对、原生审查、磁盘/权限复核和持久阶段共同承担。`--status` 的 `fileOperationsEnabled=false` 仅说明普通 CLI 不开放操作；路径、请求 JSON、任意管道和解锁参数仍拒绝。

- 每次新建规范随机 UUID 的 `\\.\pipe\opc-file-helper-<uuid>`；显式当前用户/SYSTEM/管理员 DACL，不使用默认 Everyone/anonymous 权限；禁继承、仅首个实例、最多一个客户端且拒绝远程连接。客户端使用不含创建管道实例的精确访问掩码、匿名 SQOS，不能让管道服务端模拟提权客户端。路径不是调用方任意提供的管道地址；未知/非规范 ID 拒绝，失败不自动重连。
- `Peer` 以只读查询/同步权限保留预期进程的原生句柄及创建时间，每次操作检查仍存活；双方连接后分别通过内核 `GetNamedPipeClientProcessId` / `GetNamedPipeServerProcessId` 核对实际对端，在交换消息前拒绝错连。父端适配必须消费启动返回句柄，不能退回按 PID 打开；现有 PID 入口只提供底层 OS 身份，不能单独认证父端映像。产品链还要求签名配对和父映像核对；仍不能把 OS 对端身份这一层单独称为可信提权审批，也不声称抵御特权进程或对可信进程的注入/句柄窃取。
- 固定版本和方向的二进制帧绑定两份随机 UUID 组成的本次值、冻结请求 SHA-256、精确长度与正文；正文非空且最多 1 MiB，不接受截断、额外字节、超量或摘要不符，响应也必须绑定本次请求。绑定本身无生产反序列化入口；消息尝试开始前分别原子消费发送/接收角色，成功或失败后不能复用该角色。连接及回复按所有权一次性消费；这不是跨重启防重账本，也不是模型或人工授权。
- 原生采用消息模式 overlapped I/O，普通单通道等待期限大于零且最多 30 秒；完整原生审查链另使用上节的原请求剩余绝对期限，不接受任意外部超时值。取消、等待到期或对端退出时取消**本次** I/O，并等其完成后才释放缓冲区、OVERLAPPED 与事件，避免悬空内存。取消完成等待不是 OS 卡死情况下的硬实时保证。超时/断连不自动重发，也不能说明未来业务操作已回滚；文件编排仍需独立处理未知结果与恢复记录。
- 原生测试使用真实 Windows 管道、线程及三个由父测试管理的自有子进程场景，覆盖最大帧/超量、错绑定、占名、取消/超时/断连、双向错误进程和持有旧进程句柄后的退出失效。子进程入口是仅测试的 ignored 夹具，由父测试实际执行、通过 stdin EOF 退出并 wait，异常只终止自己创建的 Child；没有真实应用、文件写入、OS 令牌调整或提权。帧篡改/边界和单次消费另有纯函数测试。本阶段新增 13 项，最新完整 Rust 门禁 133 通过、3 个顶层 ignored（两个夹具实际由父测试调用，一个既有 ConPTY 人工探针），文档/格式/差异检查通过。

签名配对、逐次人工批准、完整路径/文件事实重验、完整 SACL、持久阶段与实际替换/恢复已由上下层串联；管道层本身仍不能直接调用权限原语来绕过这些前提。无新增 crate 版本、依赖进程、数据库或迁移；真实跨权限 UAC/发布签名/安装包仍须另行验收。设计依据见 [命名管道访问权限](https://learn.microsoft.com/en-us/windows/win32/ipc/named-pipe-security-and-access-rights)与[消息及等待模式](https://learn.microsoft.com/en-us/windows/win32/ipc/named-pipe-type-read-and-wait-modes)。

### 单文件请求契约与原生审查转换（内部基础）

`src/file_operation_contract.rs` 为主桌面和独立辅助程序提供同一份严格请求契约；`workspace_file_snapshot/request_bridge.rs` / `replacement_recovery.rs` 从原生已捕获事实构造，`file_security_helper/review_request.rs` 在 OS 绑定通信后解析。可信 Tauri 命令和确认按钮只能消费本机内存快照/票据，再进入独立原生审查与 helper；CLI/HTTP/模型不能提供契约正文或操作参数。确定性测试不会实际提权或移动用户文件。

- 请求格式 `version=1`，绑定 `operation_id/review_id/root_selection_id`、`issued_at_ms/expires_at_ms`、精确 `root/path`、根/父目录 `volume/index` 与唯一 `operation`。只接受 `replace/restore_missing/undo_installed`，没有 Shell、批量、删除或 `approved` 字段；所有层级拒绝未知、重复字段及混合操作形状。相对路径与原精确快照共用同一验证器；根路径接受规范本机盘符与 verbatim-disk 形式，拒绝 UNC、设备路径、跳转、ADS、非规范名字与 `.git` 分量。格式通过不证明磁盘实际仍符合这些声明。
- 正文包含精确 `size/sha256/base64`，Base64 必须规范且与字节数/摘要一致；单份最多 256 KiB，编码后整个请求最多 1 MiB，不截断。替换前后必须为不含 NUL 的完整 UTF-8 且内容不同，保留 BOM/换行和合法空候选；恢复可以保留非 UTF-8/含 NUL 的已记录原始字节，不转换为文本。此内部上限不扩大现有 AI 单消息 32 KiB 外发范围。
- 每个既有文件另绑定 `identity/modified/metadata_sha256/readable_security_sha256`。最后一项明确只覆盖普通进程读到的 owner/group/DACL/完整性标签，**不能视为完整 SACL**。元数据指纹覆盖创建时间、普通属性与有界附带流，不包含可能被复核本身改变的访问时间，和原恢复比较规则一致。两种恢复还必须包含 `record_version=2/record_id/record_sha256`；摘要对应冻结 v2 Intent 的规范 Rust JSON，不是恢复 JSON 的防篡改签名。新完整安全记录版本仍待设计，不默默把 v1/v2 升级成已核验 SACL。
- 内部 `freeze_edit_request` 在准备尝试开始时消费原文件基线，核对目录选择/类型/期限，重新保护性读取并比较根/文件身份、修改时间与全部正文，再捕获父目录身份、普通元数据和可读权限。恢复 `freeze_request` 同样消费票据，固定记录与目录，核对完整 Intent、明确模式及双方冻结事实；漂移或失败不会复活票据，也不改写原文、候选或记录。前端不能通过这些内部方法自行提供原对象事实。
- 准备请求只使用原审查的剩余时间，不重新获得十分钟。协议期限最多十分钟，未来签发/已过期/溢出拒绝；解析后另保留不序列化的单调时钟期限，已过期对象不能因墙钟回退重新编码。辅助 `ReceivedRequest` 在通信绑定通过后再执行此解码，并在解码后、读取待批准请求或返回响应前重查期限、取消及对端存活。它只交出 `CheckedRequest`，**不产生执行许可、不替代辅助端磁盘复核或逐次人工批准**。
- 测试覆盖同一契约在两个编译目标的严格解析、最大正文对可装入完整消息、格式/摘要/路径/版本/期限及未知/重复字段拒绝；真实本机夹具验证基线/恢复转换不写文件、模式固定、到期/漂移/错误选择后的消费、晚建别名拒绝与原期限不延长；辅助端另以真实管道组合验证合法帧不能绕过非法或过期请求。最新完整 Rust 门禁 160 项通过，3 个顶层 ignored 仍为两个父测试实际调用的夹具与一个既有人工探针；文档/格式/差异检查通过。无 OS 权限调整、真实 UAC/应用 UI 或 Provider 调用。

辅助端的独立只读磁盘复核、完整 SACL、恢复记录独立核验、持久阶段、无覆盖替换/恢复及未知结果处理均已接到绑定产品入口。请求仍只是一次性中间准备物，不持有跨调用文件锁，不能绕过原生批准、动态 helper 能力和 UAC。真实跨权限验收仍待；无数据库、恢复格式或业务 API 迁移，模块整体保持部分实现。

### 单份审查文档与消费绑定（内部基础，尚非原生批准）

`src/file_operation_review.rs` 在主库/辅助程序共用严格请求之上生成不可变 `ReviewDocument`，正文仍为完整有界 Base64，不执行 HTML 或进行有损转换。原文/候选各存一份，明确 `from/to/retained`：替换是原对象到候选、保留原对象；缺失恢复是缺失到原对象、保留候选；撤销是候选到原对象、保留候选。空原文有完整的零字节声明，不冒充缺失；二进制恢复不强制变成 UTF-8。

- 文档含操作/审查/目录选择身份、精确根/相对路径、原截止时间及整个规范请求的 SHA-256；文档自身另作摘要，绑定完整内容和方向。身份、时间、权限或恢复记录摘要即使未单独展开显示，也不能在文档不变时换入另一请求。这些摘要仅做完整性绑定，不是签名、人类在场或可信进程证明。
- `PendingReview` 不可克隆/反序列化，决定方法按所有权消费实例，取消、摘要/目录不符或过期均不给继续结果；成功只移动原 `CheckedRequest`，不重新解码或延长单调期限。`ReviewedIntent` 仍不是执行批准，只能继续进入固定 helper 启动层；CLI/模型不能通过提供摘要获得写权限。
- 这是每个已持有实例的一次性消费，不是跨进程/重启的全局防重放账本。网页完整差异确认只发起操作，随后由该原生窗口再次审查；模拟决定测试不等于用户批准。可读权限与完整 SACL 继续严格区分。

新增 6 项契约测试在主库和辅助程序分别运行，覆盖三种方向、空文件/原始二进制、最大正文对、不延长期限、取消/漂移/过期以及未展示事实的整体绑定；原生快照准备测试串联文档/模拟决定后再进行磁盘复核。生产文件写入和恢复门禁、业务 API/schema079/记录格式均不变。

### 独立 Win32 完整审查窗口（已接绑定产品入口）

`file_operation_review/native.rs` 与 `text.rs` 将同一不可变审查文档接到独立原生窗口，不加载 WebView、HTML/Markdown/RTF、Shell 或模型。`show` 在专用线程创建窗口和消息循环，不接管主应用 UI 队列；只有动态 helper 能力可用、用户在页面明确提交后由 Tauri 产品命令调用，普通启动、只读浏览和 CLI 状态查询不会自动显示窗口或触发 UAC。

- 三个可滚动、可复制的只读文本区域分别显示目录/身份/摘要/原期限与限制、完整操作前后对照、保留对象。对照列出完整前后行而非最小 diff；路径/正文的换行、BOM、双向控制及其他指定不可见字符显式转义，二进制统一完整十六进制，不丢字节；缺失与 0 字节原文分别说明。代码固定按钮为“确认以上内容（仅审查）”与“取消”，窗口明确不会执行。
- 控件显式设置完整文本容量，显示前按 UTF-16 完整回读三个控件；任何截断或读取失败拒绝继续，确认时和交接前再次比对全部显示文本。原请求与文档摘要保持绑定，不接受从网页提供的替代正文。支持窗口缩放、滚动和 Tab 导航，初始焦点为取消；Escape/关闭取消，不把 IDOK 当确认。真实高 DPI、屏幕阅读器与视觉体验仍待人工验收。
- 调用方必须提供短时、非阻塞的当前目录/会话/对端/取消检查；窗口创建/显示前、250ms 定时检查、点击和最终交接时调用，并检查原请求墙钟/单调期限。该回调不应执行文件扫描、网络或模型请求，250ms 不是系统停顿下的硬实时保证。只响应所持有继续按钮的点击通知，取消/过期/范围失效/显示变化/窗口异常不给继续结果；确认后最终检查失败同样消费请求，恢复条件不能复活决定。成功仍只返回 `ReviewedIntent`，不开放写权限。
- 独有随机窗口类和同线程句柄有明确生命周期；回调只共享借用固定 Box，避免同步重入的可变别名，捕获 Rust unwind，不让其穿过 Win32。退出回收定时器/子控件/窗口类，销毁前解除数据指针；不能解除指针时终止进程以避免悬空回调。只清理自身类/上下文匹配的窗口，不操作已复用的其他 HWND。
- 这不是 Windows 安全桌面、签名证明或不可伪造的人类在场检测；不能抵御同权限恶意进程/进程内注入。SACL/恢复记录、started-only 持久执行、产品启动及跨进程绑定已接通；全局跨重启防重放和真实跨权限验收仍未完成。特权核心仍只在辅助 binary；有确认控件也不能绕过动态能力、票据、UAC 和回执门禁。

新增 8 项真实隐藏 Win32 窗口/控件测试和 2 项纯文本测试，在主库/辅助程序分别运行。覆盖只读样式与精确回读、最大文件对无默认 32K 截断、自有按钮/一次消费、取消/关闭、显示变化/范围变化、期限/最终交接、构造清理和确认后关闭。测试不调用可见 `show`，程序化通知不是人工批准；未完成真实消息循环交互、视觉/高 DPI、UAC/SACL、Provider 或安装包验收。完整 `pnpm check:rust` 215 项通过（主库 151、辅助 62、CLI 2），3 个顶层 ignored 仍为原有 ConPTY 人工探针与两个由父测试实际调用的子进程入口；之后仅补强失败后不复活断言，双目标定向检查通过。本轮只扩展既有 windows crate 的窗口/键盘/GDI/模块读取 features，没有新增 crate 版本、业务 API/schema079 或恢复格式。

### 辅助端独立磁盘复核（内部只读基础）

`src/file_operation_disk.rs` 由桌面 library 与独立辅助 binary 共用；接收严格解析但未批准的单文件请求，自己打开磁盘对象，不把调用方声明当真实文件事实。没有新增 CLI/Tauri/HTTP 入口，仍不调用提权或写入原语。

- 只读固定本机固定磁盘的根与全部祖先、父目录，核对根/父目录身份；打开文件不跟随重解析点、不请求数据写入/删除权限。先核对原生文件身份再读取正文，陌生同名对象即使内容相同也拒绝。完整字节、修改时间、普通元数据指纹和可读权限摘要必须与请求一致，捕获元数据后再核对正文/身份/时间；开始与结束检查原请求的墙钟及单调期限，不重新延长。
- 替换只读当前目标，候选仍为请求中的有界字节，不创建候选；当前原对象有硬链接则拒绝。缺失恢复读取由规范记录 UUID 派生的 `.opc-file-<id>.original` 与 `.candidate`，要求目标不存在；撤销读取原对象暂存与目标候选，要求候选暂存名不存在。恢复只核对既有对象，允许晚建别名但不移动、改写或删除。只把 Win32 `FILE_NOT_FOUND` 当缺失，目录占用、共享冲突、权限失败等都拒绝。
- `DiskReview` 持有只读文件和目录句柄，可在同一生命周期重验固定请求，不对调用方暴露文件句柄；辅助接收层另包装通信借用，重验前后检查对端存活、取消和期限。缺失名字只是瞬时观察，不是预留；并发创建仍可发生，未来执行必须再次核验并用不覆盖操作，不得把只读复核当写许可。
- 明确未证明的事实：这里的普通可读权限仍不包含完整审计 SACL。辅助端固定存储、独立重读记录并与请求比对已补齐，但只证明当前观察一致，不认证历史；普通端准备不能替代辅助端完整权限复核，记录也不能替代磁盘事实。逐次批准与持久执行回执已串联，真实签名发布/UAC/SACL 验收仍待；不声称抵御管理员、原始卷访问或既存可写映射。
- 复用原有 Windows 路径/读取、普通元数据和可读权限实现，避免父/辅助两份规则漂移；快照身份统一使用拒绝未知字段的共享结构，合法字段与序列化格式不变。权限特权核心仍只编入辅助 binary，不进入主桌面。

本轮新增 9 项真实临时文件测试，分别在主库和辅助程序运行：无写权限/路径固定、陌生超大文件先拒绝身份、正文/时间/ADS/权限声明差异、目录替换、晚建硬链接、两类恢复位置及并发占用、候选变化和过期。原生快照及两种恢复票据转换测试已串联到该复核层；管道组合测试也验证有效结构不能冒充磁盘事实。ADS 写入会同时更新时间，测试显式恢复主文件时间来独立验证元数据漂移拒绝。完整 Rust 门禁 183 项通过（主库 135、辅助 46、CLI 2），3 个顶层 ignored 为原有人工 ConPTY 探针及父测试实际调用的两个子进程夹具；新增普通权限声明测试不等于真实 SACL/UAC 验收。恢复记录格式、数据库 schema079、业务 API 和运行时可用范围不变。

## 项目文件精确基线（2026-09-22，内部基础能力）

受限项目文件读写链的精确读取基础层已经实现，随后接到 [主聊天单消息分析](ai-assistant.md#精确项目文件的单消息分析2026-09-22已接源码)。`workspace_file_snapshot/validate/release` 沿用可信 `main` WebView 调用门禁和已选 root ID；绑定发行构建另可在明确确认后消费快照进入本页首节的辅助程序替换链，普通开发构建仍只读。没有数据库迁移。现有只读预览和旧人工快照交接保留，新入口独立提示完整外发、Provider 与会话授权，不把打开预览当授权。

- 精确读取只支持 Windows 本机固定磁盘的普通、无硬链接、有效 UTF-8 且不含 NUL 的文件，单文件最多 256 KiB；不截断、不替换解码，保留 BOM、CRLF 和末尾换行。网络/设备根路径、路径跳转、非规范 Windows 文件名、ADS、`.git` 路径分量、符号链接及目录联接拒绝。这个上限是本地基线上限，不承诺完整内容可装入某次模型请求。
- 返回 `id/path/content/size/sha256/expiresInSeconds`；`path` 是相对路径，`size` 是 UTF-8 字节数。快照 ID 和正文只供本地使用，不是 AI 授权。绝对根路径和原生文件身份不在响应中。最多保留八项，每项有效十分钟；捕获或复核时惰性清理过期项，释放幂等，重启清空，无持久化。调用方若取消在途读取，收到迟到 ID 后仍需释放。
- 读取期间通过不共享删除的目录句柄固定祖先路径，用不跟随重解析点的打开方式逐项检查；文件句柄拒绝共享写入/删除。读取前后检查文件信息。快照保存根目录身份、文件身份、修改时间及精确内容；复核重新读取并比较，变化或读取失败后旧快照永久失效，文件恢复成旧内容也不能复活。错误不输出本地路径。
- `validate` 只证明此次读取时仍相同，不持有跨调用文件锁，不能当作最终写许可。未来确认写入必须在自己的受保护文件操作中重新验证并完成恢复登记。此机制不声称能抵御管理员、原始卷访问或已存在的可写内存映射，也不构成通用 OS 沙箱。

代码：`apps/desktop/src-tauri/src/workspace_file_snapshot.rs`、`apps/web/src/api/workspace.ts`。后续读取链由 `aiProjectFiles.ts`、`AiProjectFileContext` 与 `ai_project_file_context.go` 接通，UI 当前单文件、外发最多 32 KiB，发送前复核且不把 root/snapshot ID 发给模型。`aiProjectFileReview.ts` 现可在消费外发授权后暂留同一精确基线，用于主聊天完整差异审查；不延长期限、不开放写入命令，人工“复核当前文件”仍只证明瞬时一致，失败/身份变化/丢弃/过期释放。待完成：模型自主目录读取、安全的逐次批准写入及恢复、真实桌面与 Provider 验收。不能将“读取与建议审查已接通”描述为“AI 可改项目文件”。

本切片验证：十二项原生定向测试覆盖精确字节/摘要、非法编码/超限、硬链接、目录联接、并发写句柄、文件及根目录替换、删除、目录绑定、过期与容量；真实测试联接由 Win32 在隔离临时目录创建，不启动 Shell 或要求管理员。`pnpm check:rust` 完整通过（60 通过、1 项既有 ConPTY 人工探针忽略），三项前端接口测试、类型检查及文档检查通过。测试临时目录已清理；这不是桌面交互、真实模型或写入恢复验收。

H5-E35 为智能体请求的当前 Chromium 标签动作补充本地回执：只有人工确认后原生 `browser_action` 无错误返回，右侧才显示“命令已接受”，不宣称网页加载完成；失败保留原请求。用户可再经聊天交接卡与手动发送共享无地址/无正文的固定结果，切换标签或新请求会撤销旧回执。Sidecar 工具本身不收执行结果，原生浏览器安全边界不变。见 [AI 助手](ai-assistant.md#浏览器动作的本地执行回执与显式交接h5-e352026-09-21)。

> 桌面基座最初以 app v0.1.0 / API v1 / SQLite schema v43（2026-08-29）交付；当前仓库为 app v0.1.1 / API v1 / schema 78。数据库父目录运行锁、启动阶段恢复进度、generation-aware 内置 Sidecar 有界自动恢复、父管道 EOF 退出、前端世代清理、安全应用重启、托盘显示/隐藏/显式退出、持久化关闭到托盘偏好，以及运行诊断能力快照已实现；Windows x64 曾完成原生链接、38 项 Rust 测试和未签名 NSIS/MSI 本地打包，但这不证明当前构建已完成同等专项。T-02 仍部分完成，安装后托盘交互、真实父崩溃/进程树、签名、干净系统与其他平台验收仍待完成。当前阶段只规划签名离线更新，不启用在线 Updater。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v10.47](../opc-workspace-PRD.md) · [数据管理](data-management.md) · [任务](tasks.md) · [本地提醒](reminders.md)

## 定位与边界

本模块负责把 React 前端、Go Sidecar 和 SQLite 作为一个可安装、可启动、可恢复、可退出和可升级的本地桌面产品交付。它提供操作系统能力和进程生命周期，不拥有任务、收件箱、专注或备份等业务事实。

核心边界：

- 桌面运行时采用 Tauri + 内置 React + 随安装包交付的 Go Sidecar，不要求用户安装 Node、Go、Rust 或 Docker。
- Sidecar 只监听 127.0.0.1 动态端口，生产请求使用启动期随机会话令牌和 Origin 校验。
- 核心功能在断网环境完整可用。
- 平台能力按最小权限逐项启用；不可用或未授权时必须降级并解释。
- 更新以桌面壳、前端资源和 Sidecar 的同版本签名离线包为单位。
- 在线 Updater、联网更新检查和静默下载不在当前阶段，未来若引入必须新增 ADR、网络权限、失败回退和用户开关。
- 三平台支持只能在对应安装包、签名、公证、干净机和性能测试通过后声明。

## 当前实现状态

已实现：

- React `AppShell` 宽屏左侧主导航可通过品牌区右侧按钮在 220 px 展开态与 64 px 图标态间切换；收起态保留搜索、全部模块、设置与原位展开入口的可访问名称/悬浮提示。窄屏继续自动使用 64 px 图标栏并隐藏重复按钮；该状态为当前运行期短期 UI 状态，不进入业务数据库。
- Tauri 2 桌面窗口、固定最小尺寸和内置 Web 前端。
- single-instance 插件；再次启动时显示、取消最小化并聚焦主窗口。
- Tauri `tray-icon` 已提供最小托盘：只有托盘构建成功且 committed `close_to_tray` 启用时才拦截主窗口关闭并隐藏；左键和固定“显示 opc-workspace”恢复窗口，固定“退出 opc-workspace”进入应用退出与 Sidecar 优雅关闭。构建失败或偏好关闭时关闭请求按原路径继续。配置的应用图标不可用时使用代码内本地 RGBA fallback，不读取外部路径。
- 生产配置通过 externalBin 打包 opc-sidecar，开发可通过 OPC_SIDECAR_URL 连接外部 Sidecar。
- `src-tauri/icon.svg` 作为可维护图标源，已生成 Windows `icons/icon.ico`、macOS `icons/icon.icns` 与所需 PNG 尺寸；`bundle.icon` 显式声明跨平台图标列表。
- 当前 Windows x64 已补齐 Visual Studio C++ Build Tools 与 Windows SDK，`cargo check`、`cargo test` 和 Tauri 的 `targets: "all"` 均通过，产出未签名的 NSIS `.exe` 与 MSI 本地测试包。
- Tauri 获取 appDataDir 和 appLogDir，创建数据库、附件、Artifact、发票、备份和配置目录。
- 内置 Sidecar 每个 generation 都生成新的随机会话令牌，并以 `127.0.0.1:0` 重新请求 OS 分配动态端口；端口值允许被 OS 复用。
- Tauri 将 `appDataDir/opc-workspace.db` 作为 `OPC_DB_PATH`，将 `appDataDir/artifacts/` 作为 `OPC_ARTIFACT_DIR` 传给 Sidecar；Sidecar 默认从数据库父目录解析已由 Tauri 创建的 `appDataDir/backups/`。生产路径不出现在命令行，也不写入持久前端配置。
- Go Sidecar 在检查 pending restore、执行迁移或打开 SQLite 前，先在数据库父目录对固定 `.opc-sidecar-run.lock` 获取非阻塞 OS 独占运行锁；冲突立即失败且不接触数据库。锁文件退出后可保留，所有权只由 OS lock 表示。
- Sidecar 随后校验 Artifact root marker 的 `format_version / database_id / store_id`，用不可变数据库 ID 与一次性 `artifact_store_id` 做双向绑定，获取并持有独立的 Artifact root 进程锁，再依据 Artifact 事实和 immutable deletion tombstone 协调 `.staging/`、`objects/`、`.trash/`、`.quarantine/`；数据库运行锁和 Artifact root 锁分工不同，均不能省略。
- 解析 stdout ready JSON，拒绝非 loopback、端口 0、带凭据或带额外路径的地址。
- Sidecar 在 ready 前可输出 `{"event":"startup","stage":"…"}`；Tauri 严格只接受锁、恢复包校验/应用/复验/收尾、数据库、迁移、工作区和本地 API 的固定 stage code，并映射为恢复页文案。未知 JSON、未知 stage 或非 ready 协议行会终止本代；状态不保留本机路径、备份 ID、令牌、原始错误或用户输入。
- Tauri 原生健康探测与前端 sidecar_status 连接握手。
- `sidecar_status` 当前只使用 `starting / restarting / ready / error`，为受管内置 Sidecar 返回 `generation`，并可在 `starting` 返回白名单 `startupStage`，同时返回 app/API/schema 版本。
- 已启动 generation 只有真实 `Terminated` 才触发下一代，自动恢复最多 2 次，退避固定为 500 ms、2 s；当前 generation 连续 Ready 30 秒后重置预算。外部模式、显式 shutdown、事件流关闭但没有 `Terminated` 都不会自动重拉；内置二进制定位/child spawn 失败可在同一预算内重试。
- React 根节点的桌面服务恢复闸门：`starting / restarting` 时不渲染业务页，ready 后放行并持续观察，error/读取失败时显示不含原始 message 的全局恢复页。从 ready 进入非 ready 会清除运行期连接、取消并清空 TanStack Query；ready generation 变化也会覆盖漏过中间 `restarting` 的轮询。浏览器开发模式直接放行。
- 应用退出时向精确子进程写入 shutdown，最多等待 7 秒，超时后终止；Sidecar 优雅关闭 HTTP、checkpoint WAL 并关闭数据库。并发 shutdown 调用共享同一次 stop，后续调用等待第一次完成；若 ready 等待恰在 shutdown 已取走 child handle 后超时，握手任务不会伪造 exited。Tauri 还为内置模式注入 `OPC_EXIT_ON_STDIN_CLOSE=true`，父控制管道 EOF 触发同样的 Go 优雅退出；外部/开发默认 false。
- 恢复计划挂起后，设置页可调用 `restart_application`：若受管 child 存在，只有真实 `Terminated` 的 code 0 且无 signal 才允许 Tauri 重启应用；内置启动失败尚未创建 child 时允许继续，延迟到达的干净退出确认后可再次请求。外部 Sidecar、非零退出、signal 或未确认退出都会拒绝。
- Tauri capability 当前仅开放 core:default；前端不能任意调用 shell。
- manual Task 文件产出通过 WebView 文件选择与鉴权 multipart 上传进入 Sidecar 受控目录；前端不能指定服务端 `relative_path`，下载也只能经过鉴权 content API。

尚未实现或仍待真实验收：

- 真实 Tauri/Sidecar 父进程崩溃、进程树和安装包生命周期验收；Windows x64 本地包已生成，但尚未在干净系统完成安装、启动、卸载与数据保留验证，macOS/Linux 也尚未构建。当前没有 Windows Job Object、Unix 进程组或孙进程治理。hard-hung orphan 只会继续持有数据库运行锁并阻止新 Sidecar 接触同库，不会被自动识别或回收。
- 数据库打开前的备份选择与安全回滚交互（H-HH02，2026-09-23）：全局恢复页 error 态提供“从备份恢复 / 跳过并重启”。列表由 Tauri 只读扫描 backup root 的 `manifest.json`，最多 20 项 metadata-only（id/时间/kind/校验状态/schema/有界 note）；用户选定后二次勾选确认，经 Sidecar 一次性 `prepare-restore --backup-id` 安排 pending restore（创建回滚包并发布 plan），成功后提示重启在打开业务数据前应用。失败关闭、不同 pending 拒绝、不展示路径/原始错误，不启动 HTTP API。跳过保持当前数据。真实安装/UAC/断电仍待专项验收。
- `OPC_LOG_DIR` 已用于启动前安全故障 journal、下一次健康启动补偿和 Go Sidecar/Tauri 壳脱敏轮转日志；设置“运行诊断”可白名单化展示桌面生命周期/版本、托盘运行时可用性，复制基础脱敏摘要、下载诊断包 v1 并打开自身日志目录。`desktop_capabilities` 对未接入集成只返回 `not_implemented`，读取失败不阻断生命周期诊断；诊断包包含版本/平台、SQLite 健康/迁移和维护错误码汇总，原始日志不进入该包。
- 托盘专注状态/快捷业务动作、原生通知、其他 OS 全局快捷键、开机启动和业务文件对话框；关闭到托盘设置开关已接，基础托盘已在当前主机完成原生编译，实际关闭/恢复/退出交互与 Sidecar 无残留仍待验收。
- 签名离线更新包的选择、验签、迁移前备份、安装与回退。
- Windows/macOS/Linux CI 构建、代码签名、公证、安装包和干净系统验收；当前仅完成 Windows x64 的未签名本地包。
- 当前主机上的 Rust 格式、`cargo check`、38 项 Rust 单元测试与 Windows x64 Tauri 打包已经通过，但仍不能替代真实 Sidecar 生命周期、安装后交互、干净系统或三平台支持。

## 目标功能

### 智能体右侧人工作业工作区（2026-09-17）

本次独立 AI 轨道新增 `AgentWorkspace`，替换固定“概要/子智能体/文件/浏览器”栏。顶部 `+` 可新建、切换、关闭变更审查、终端、浏览器、文件、侧边聊天、子智能体、受控附件及环境信息标签；支持双工具分栏、放大/还原、拖动外侧分隔条、键盘切换标签与收起。浏览器标签直接并入统一标签栏，不再嵌套第二条浏览器标签栏；弹出网页同步新标签。最多 24 个普通工具标签，浏览器仍按原生 12 标签上限，分栏只允许同时显示一个原生网页。`子智能体` 标签额外只读镜像当前主对话的已保存计划状态（标题、版本、步骤计数、实时状态及未替代步骤的标题/状态），并可返回原计划、精确建议或经本地白名单验证的工作台记录；读取失败明确显示并可重试。它不显示审批预览/产出正文，也不在右栏发送、授权、确认、启动、取消、重试或恢复任何操作。

- 文件：原生选择目录后授予进程内 root ID；按目录浏览与筛选，只读文本/源码（最多 2 MiB，前端最多 10,000 行，超出显示截断）、PNG/JPEG/GIF/WebP/BMP（最多 10 MiB）预览。HTML/SVG/Markdown 显示转义源码；PDF/Office 和其他二进制明确提示不支持，不使用 iframe 执行本地内容。原有任务产出/附件入口保留，非文本受控附件仍只显示元数据。
- 审查：选择独立 Git 仓库根目录，查看未暂存/已暂存文本差异、当前分支与状态列表；手动刷新，不调用暂存/提交/推送。外部 `.git` 的 linked worktree 暂拒绝，未跟踪文件不生成 diff，可用文件预览查看。Git 需在 PATH 可用，失败/超时/超限显式展示。
- 终端：Windows ConPTY + PowerShell + xterm，手动启动/输入，支持 ANSI、交互输入与缩放。当前用户权限、可访问目录外及网络，不宣称沙箱；AI 无输入权限。H4-AO 仅在用户点击“交给智能体分析”、交接卡再次带入并发送后，才把 React 近期捕获的有界输出尾部作为普通聊天文本外发；不含 PTY/root ID、工作目录、路径、环境或输入，AI 也不能续读、写入、调整或关闭终端。Job Object 负责子树终止；收起/切页保留，关闭标签确认后结束，退出应用全部结束。最多 4 个，1 MiB 输出环，不落盘；其他平台尚未启用。
- 侧边聊天：独立会话与草稿，复用原有 SSE 控制器（主/侧共用一次生成），侧边生成不切走主会话。原有 persist 偏好适用；任务/记忆确认通过“在主对话中打开”处理。没有文件/终端/网页自动上下文。

新增 native commands：`workspace_choose_root`、`workspace_list`、`workspace_preview`、`workspace_git_review`、`workspace_terminal_start/read/write/resize/close`（后五项分别为对应完整前缀命令）。全部只接受可信 `main` WebView 调用，不暴露 Sidecar HTTP 路由。目录授权、面板状态与输出仅在内存，重启不恢复；无 schema/API 迁移。详见 [ADR-028](../adr/028-human-operated-agent-workspace.md)。这不是 Agent Runtime 的 Shell 能力，也不改变现有 Sidecar 子进程治理限制。

确定性门禁与原生探针分开：默认 Rust 测试不启动终端；显式运行 `cargo test --manifest-path apps/desktop/src-tauri/Cargo.toml real_powershell_pty -- --ignored` 已在当前 Windows 主机验证光标握手、输入输出、resize 与关闭。真实远程模型、其他平台、完整安装包仍需分别验收。

### 启动与服务恢复

- 建立明确桌面状态机：初始化目录、启动 Sidecar、ready 握手、健康检查、兼容校验、可用或恢复。
- 检查桌面应用、Sidecar、API 和 schema 版本兼容，阻止错误组合进入写入模式。
- 启动超时、ready 格式错误、健康失败或迁移失败时显示专用恢复页。
- 恢复页已支持状态重查、打开脱敏日志、查看白名单版本、受控应用重启、数据库打开前的白名单恢复/迁移进度，以及有界“从备份恢复 / 跳过并重启”选择（详见上文 H-HH02）；检查数据目录仍待实现。
- 已实现内置 Sidecar 的两次有界自动重启、固定退避和 30 秒稳定预算重置；重试耗尽后进入 error 并要求用户处理。
- 新 generation 只在旧代真实 `Terminated` 或 child 尚未创建的 spawn failure 后启动；数据库运行锁进一步阻止未知旧进程与新进程同时接触数据库。

### 退出与孤儿治理

- 区分关闭窗口、最小化到托盘和真正退出。
- 真正退出先停止接受新业务操作，等待短事务，并让可取消的长任务进入 cancelled/interrupted 后再优雅关闭 Sidecar。若数据恢复已进入不可取消的 applying/restarting 阶段，桌面必须阻止普通退出，等待恢复协调器完成或回滚。
- 维护精确子进程句柄和 generation，不使用宽泛进程名终止其他实例；并发 shutdown 共享一次 stop。
- 父进程异常退出时，Go 受管模式可由父控制管道 EOF 优雅结束；若进程 hard-hung，下一次启动只会由数据库运行锁拒绝接触同库，当前不会识别、终止或回收孤儿/孙进程。
- Agent 子进程由本地 Agent 模块管理，但最终退出清理纳入桌面生命周期。

### 系统托盘

- **当前已接源码**：托盘成功建立且设置启用时关闭主窗口隐藏，左键/“显示”恢复，“退出”复用优雅关闭；托盘失败或设置关闭时不拦截关闭。
- **当前已接设置**：通用设置点击后立即预览下一次关闭行为，保存写入 SQLite，取消恢复打开时 committed；首次提示仍待后续。
- **后续**：菜单增加快速新建任务、开始/暂停专注和设置。
- **后续**：图标区分空闲、专注、休息和本地服务故障。
- 退出动作必须关闭 Sidecar 和数据库；隐藏窗口不得误触发退出。

### 原生本地通知

- 支持任务、收件箱、专注、发票和系统维护的本地通知。
- 首次使用前解释用途并请求最小系统权限。
- 点击通知打开对应本地资源详情。
- 通知不可用或被拒绝时保留应用内提醒，不影响业务状态。
- 当前“应用内提醒”已由 Sidecar Reminder 到期生成 Inbox Item；操作系统通知权限、通知中心和点击通知跳转仍属于后续系统集成。
- 当前阶段不发送远程推送、邮件或第三方消息。

### OS 全局快捷键

- 已尝试注册命令面板 `⌘/Ctrl+Shift+K` 与新建任务 `⌘/Ctrl+Shift+N`；触发时显示/聚焦主窗口，再向 `main` WebView 发出固定 `command_palette/new_task` action。
- 注册失败、权限拒绝或与系统冲突时只报告 `unavailable`，并保留 WebView 内快捷键；运行诊断不显示原始平台错误。
- 开始/暂停专注和页面切换快捷键仍待后续。
- 系统快捷键只触发打开界面或安全动作，不能绕过确认、验收与权限。
- v0.3 再支持用户自定义和冲突调整。

### 文件对话框与路径授权

- 当前 Task 文件 Artifact 使用 WebView 文件选择器取得浏览器 `File`，以 multipart 上传；Sidecar 只接收字节流并复制到受控 Artifact store，不接收客户端本机绝对路径。
- 导入、导出、恢复、附件、发票和 Adapter 选择均使用原生文件对话框。
- 前端只获得受控文件引用或经过校验的用户选择，不接受任意路径字符串直接读写。
- 路径在 Rust 和 Sidecar 边界规范化，防止目录逃逸和符号链接攻击。

### 日志与诊断

- Sidecar 与 Tauri 壳已分别写入 appLogDir 的脱敏轮转日志；桌面壳只记录白名单生命周期 JSONL。
- Sidecar 日志保留受控进程阶段、版本、错误码、HTTP 路由模板、request ID 和耗时；桌面壳日志只保留生命周期事件与时间。两者均不包含令牌、完整客户资料、发票正文或 Agent 输入输出。WebView→Sidecar request ID 已实现；Tauri 壳不在 HTTP 请求路径上，不向生命周期事件伪造 request ID。
- 诊断页显示当前状态、最近失败和路径，支持复制脱敏摘要与打开日志目录。
- 系统维护失败可以幂等生成本地收件箱项。

当前已交付启动故障安全层和双进程日志纵切：Tauri 传入 `OPC_LOG_DIR`，Sidecar 也支持 `--logs` 和数据库同级默认 `logs/`；数据库启动/迁移及 Sidecar 启动失败写白名单 journal，下一次成功启动在 ready 前补偿为 Inbox Item。Sidecar 同时写 stderr 与 `opc-sidecar.log`，单文件最多 5 MiB、保留 `.1`～`.3` 三份归档；最终写入层遮盖会话令牌和 Bearer 值，访问记录不含 query/header/body，文件失效后降级 stderr。Tauri 壳以独立文件记录白名单生命周期事件。设置诊断页与脱敏诊断包 v1 已交付且不包含原始日志；无参数 command 可打开自身 `appLogDir`。WebView→Sidecar request ID 串联也已交付。

### 签名离线更新

- 用户主动选择本地签名更新包。
- 应用验证签名、版本、平台、架构和兼容范围。
- 更新前等待写事务、创建并验证一致性备份。
- 正常关闭 Sidecar 后由安装程序替换桌面壳、前端和 Sidecar。
- 新版本启动检查版本并迁移；失败进入恢复页。
- appDataDir 与安装目录分离，正常更新不覆盖业务数据。
- 不进行在线检查、后台下载或静默更新。

### 构建与发布

- CI 构建 windows-x86_64、darwin-x86_64、darwin-aarch64 和 linux-x86_64。
- 每个 Tauri 包内置匹配 target triple 的 Sidecar。
- Windows 代码签名，macOS 签名与公证，Linux 发布 SHA-256。
- 在无开发工具和 Docker 的干净系统验证安装、首次启动、备份、更新、卸载后数据保留和彻底删除入口。

## 关键用户流程

### 正常启动

1. Tauri 获取单实例锁并初始化 appDataDir / appLogDir，包括 `artifacts/` 目录。
2. 为本 generation 生成新令牌并以端口 0 启动内置 Sidecar，同时注入 `OPC_EXIT_ON_STDIN_CLOSE=true`。
3. Sidecar 在每个数据库打开前阶段向受管父进程写入固定 stage code：取得数据库运行锁、检查或验证/应用/复验 pending restore、打开/迁移数据库、初始化工作区和启动本地 API；之后读取数据库身份、校验/创建绑定 marker、获取 Artifact root 独占锁并协调受控 store，全部成功后才输出 ready。任一进度码都不含路径、备份 ID 或原始错误。
4. Tauri 校验 loopback 地址并携带令牌调用 /health。
5. 版本与 schema 兼容后，WebView 取得当前进程内连接信息并加载业务页面。
6. 核心功能从首次启动起可离线使用。

### Sidecar 启动或运行失败

1. 初次启动失败或已启动 generation 的真实 `Terminated` 使桌面进入 `restarting`，停止业务写入并清除旧连接/查询。
2. 内置模式分别等待 500 ms、2 s 重试，最多两次；外部模式、显式 shutdown 或没有 `Terminated` 的事件流关闭不自动重试。
3. 两次仍未 Ready 则进入 error 并显示恢复页，不在后台无限重启；当前 generation 连续 Ready 30 秒才恢复下一轮完整预算。
4. 用户可手动重试、打开日志、检查版本或进入备份恢复。
5. 恢复成功后以前端可识别的新 generation、新会话令牌和重新申请的动态端口建立连接；即使轮询漏过 `restarting`，generation 变化也会触发一次清理与业务树重挂。

### 关闭窗口与退出

1. 当前托盘可用且 `close_to_tray` 启用时，用户关闭主窗口会隐藏并保持 Sidecar；设置关闭或托盘不可用时不拦截关闭。设置页点击即预览，取消恢复 committed。
2. 用户从托盘或菜单选择“退出”。
3. 应用冻结新长任务，等待写事务并通知专注/Agent 模块处理运行态。
4. 若数据恢复处于 applying/restarting，显示不可退出的维护状态，待完成或回滚；其他可取消任务按其协议结束。
5. Tauri 请求 Sidecar 优雅关闭；并发调用共享一次 stop，超时后只终止精确 child generation。父管道 EOF 也会让受管 Go Sidecar 进入优雅关闭。
6. 数据库 checkpoint 完成，进程退出且无残留。若操作系统强制终止恢复阶段，下次启动必须依据恢复 journal 完成或回滚，不能直接打开不确定数据库。

### 安装签名离线更新

1. 用户在设置中选择本地更新包。
2. Tauri 验证签名、版本、平台和架构，并展示变更与兼容风险。
3. 用户确认后，数据模块创建更新前一致性备份。
4. 应用关闭 Sidecar，安装程序原子替换程序文件。
5. 新版本启动并执行兼容迁移。
6. 失败时进入恢复页；数据库兼容时可回退应用，否则从更新前备份恢复。

## 数据/API/状态与事件

### 当前与目标状态

当前 `sidecar_status` 支持以下四个 phase；受管内置模式同时返回 generation，外部模式为 null：

- starting：本地服务正在启动或等待健康。
- restarting：内置 Sidecar 已安排有界自动恢复，业务连接不可用。
- ready：连接信息和版本可用。
- error：启动、握手、健康或运行失败。

目标桌面协调状态建议细化为：

| 状态               | 含义                                 |
| ------------------ | ------------------------------------ |
| initializing       | 创建目录和读取启动配置               |
| starting           | 启动 Sidecar 并等待 ready            |
| checking           | 健康、版本和 schema 兼容检查         |
| ready              | 业务可用                             |
| restarting         | 有上限自动或手动恢复中               |
| maintenance        | 备份、恢复、迁移或离线更新中         |
| incompatible       | 组件或 schema 不兼容，只允许恢复操作 |
| error              | 需要用户处理                         |
| stopping / stopped | 正常退出阶段                         |

前端只能依据桌面层和 Sidecar 返回的状态，不通过请求失败次数自行创造第二状态机。

### Tauri command 与事件

当前 command：

- sidecar_status：返回当前 phase、generation、运行期 API 地址、会话令牌和版本。
- restart_application：无业务参数；受管 child 存在时只接受 code 0 且无 signal 的真实退出，尚未创建 child 的内置启动失败可继续，延迟干净退出后可重试。浏览器开发模式、外部 Sidecar、非零/signal/未确认退出都会拒绝。
- open_log_directory：无业务参数，只打开应用自身日志目录。
- desktop_shortcut_status：只返回两个固定原生快捷键的 `registered / unavailable` 状态。
- desktop_capabilities：只返回托盘、原生通知、自启、原生文件对话框和离线更新的 `available / unavailable / not_implemented` 白名单枚举；当前只有托盘可在初始化成功后为 available。
- set_close_to_tray_enabled：只接受 `enabled: bool`，更新当前进程内的关闭决策；持久事实仍由 SQLite `app_settings.general.close_to_tray` 拥有，启动与设置预览负责同步。
- open_external_browser / close_external_browser：保留的独立原生窗口接口；右侧浏览器已经改用下述原生多标签命令，不再调用它们。
- browser_snapshot / browser_create_tab / browser_activate_tab / browser_close_tab / browser_navigate / browser_action / browser_capture_page_text / browser_set_layout：Windows 智能体右侧内嵌浏览器，契约见下一节。

规划 command 或事件职责：

| 能力                        | 职责                                  |
| --------------------------- | ------------------------------------- |
| select_import / export_path | 原生文件选择和受控路径授权            |
| sidecar-state-changed       | 向 WebView 推送状态变化               |
| desktop-global-shortcut     | 已注册的命令面板或新建任务固定 action |
| notification-activated      | 打开对应本地资源                      |

正式名称在实现 ADR 中冻结。所有高风险命令限制到 main 窗口的最小 Tauri capability，并验证调用参数。

### 内嵌 Chromium 多标签浏览器

H5-E31 仅在当前单消息另选 `workspace_browser` 后，允许智能体向右栏提出后退/前进/刷新/停止加载请求；右栏显示本机活动标签地址，人工确认时重新核对原生快照才调用既有 `browser_action`。同一范围的新网址请求只有原生创建成功后才从本地确认卡消失，失败保留并可重试；标签满 12 个时需先关闭一个。普通网页模式不能执行该标签命令，Sidecar/模型不自动读取或收到标签、页面及结果；H5-E35 只允许用户手动带回无网页内容的本地成功回执。H5-G37 新增另一个独立用户动作：只对当前加载完成的活动标签运行固定文本捕获，并把最长 8 KiB 的可见正文与脱敏地址先显示在交接卡中；H5-G38 再在同一快照内提供最多 20 个唯一可见 HTTP(S) 链接，目标地址移除 query/fragment，正文最多 8 KiB、链接列表最多 4 KiB。用户仍须再次带入、手动发送；任何页面内容均不自动进入模型。手动浏览器操作仍不需要智能体授权。见 [AI 助手](ai-assistant.md#当前浏览器标签的逐次确认操作h5-e312026-09-21)与[显式链接交接](ai-assistant.md#内嵌-chromium-可见链接的显式交接h5-g382026-09-22部分实现)。

本功能属于 AI 助手独立轨道；最初引入浏览器时为 schema 71，当前仓库为 v0.1.1 / API v1 / schema 78，H5-E35 不新增迁移。Windows 桌面版使用 WebView2（Chromium）子视图真实内嵌网页，不使用 iframe，也不把外部页面改开独立窗口；要求 WebView2 Runtime 122.0.2365.46 或更新版本，以支持涵盖 Service Worker 的资源隔离接口，缺少接口会拒绝创建并提示升级。其他平台和普通网页开发模式不提供原生内嵌能力；网页开发模式可明确点击外部打开。没有迁移整个应用到 Electron，也没有打包第二份 Chromium。

- **入口与布局**：智能体模式打开右侧工作栏，选择「浏览器」。标签条提供新建、切换、关闭，地址栏提供前进、后退、刷新／停止；普通主机名补 HTTPS，回环地址补 HTTP。宽度独立于概要面板，默认 680px，360–1200px 内调整，并给聊天保留至少 360px。宽度不足 961px 时隐藏右栏；桌面最小窗口 1080px 仍可浏览。
- **真实状态**：Rust `BrowserState` 管理最多 12 个标签。每项含 `id/url/title/loading/canGoBack/canGoForward/error`；快照含 `tabs/activeTabId/revision`。地址、标题、历史与加载错误取自 WebView2 事件，前后退调用原生历史接口。关闭当前标签选择邻近标签，关闭最后一项回到空态；重开面板回读进程内快照。
- **命令**：`browser_create_tab({url?})` 的空地址或 `about:blank` 创建新标签；`browser_navigate({tabId,url})` 只接受有效、不含账号密码的 HTTP(S) URL；`browser_action({tabId,action})` 只接受 `back/forward/reload/stop`；activate/close 接收 `tabId`。除布局外命令返回完整快照，事件 `browser-state-changed` 只发送到主 WebView。新窗口请求失败通过同样仅发给主 WebView 的 `browser-notice {tabId,message}` 提示，保留父标签的正常网页；提示条可关闭，不作为页面导航错误。
- **显式页面捕获（H5-G37/G38）**：`browser_capture_page_text({tabId})` 只由可信主 WebView 用户点击触发，要求精确活动标签为加载完成、无错误、有效 HTTP(S) 且非应用/Sidecar 内部来源。Rust 在串行操作锁内预检并再次调用 `capture_target`；原生代码读取固定正文/链接提取脚本，正文最多扫描 100,000 个字符 / 8 KiB UTF-8；链接最多扫描 1,000 个 DOM anchor，输出最多 20 个唯一、已渲染且非隐藏的 HTTP(S) 目标，目标地址去 query/fragment，含 username/password 的目标直接拒绝，链接文字与 URL 合计最多 4 KiB。WebView2 异步回调最多等待 8 秒，捕获后重新比较活动标签、原生 URL 与导航代次。返回只含 `tabId/url/title/text/truncated/links/linksTruncated` 的瞬时结果，前端只在内存交接卡展示全部实际发送字段；网页地址与链接目标均去 query/hash，含敏感路径时由用户在发送前审阅。再经“带入网页内容”和正常手动发送才可到模型。网页没有任意脚本或可调用 DOM API，不读取表单值、Cookie、存储、截图或下载；Sidecar/模型不触发捕获。任何网页内容均按不可信输入处理，导航仍须另行 `workspace_browser` 单消息授权及逐次人工确认。
- **布局与生命周期**：`browser_set_layout({bounds})` 接受 CSS 像素 `{x,y,width,height}`，Rust 按主 WebView 缩放与窗口 DPI 换算并裁剪；`null` 隐藏全部网页。当前仅显示活动标签。拖拽分隔条、应用弹窗、面板卸载、切换工作台或页面不可见时隐藏原生表面，避免它覆盖 HTML 弹层；恢复后重新测量。切换或隐藏不会销毁标签，后台页可能继续运行；关闭标签释放对应视图，退出应用统一回收。前端隐藏请求绕过慢布局队列，epoch 使旧显示请求失效，owner lease 防止旧卸载清理覆盖新面板。
- **权限与数据**：主界面 capability 只匹配 `webviews:["main"]`，不能以 `windows:["main"]` 给所有子视图授权；所有自定义 command 额外检查主视图调用者，child 关闭 WebMessage。导航及重定向校验协议与受保护应用来源。网页无 Sidecar 凭据、不能读写任务或执行应用命令；H5-G37/G38 仅在用户点击后通过专用固定脚本捕获有界正文/链接，卡片再次预览且单独手动发送，绝不自动发送或开放网页能力；不会读表单值、Cookie、Storage 或历史。浏览器独立 profile 在标签间共享站点 Cookie，和工作区 WebView 分离；开发默认 `.local/browser-profile/`，正式版为 `appLocalDataDir/browser-profile/`，不进入业务 JSON/ZIP 或 SQLite 备份。标签列表仅在本次应用进程保留，重启不恢复标签。
- **边界**：这是嵌入式浏览器工作区，不承诺 Chrome 扩展、书签同步或完整浏览器设置。普通 `target=_blank` 链接进入新标签；依赖 opener、POST 或特殊窗口上下文的登录弹窗需要单独兼容验收。Ctrl+L/T/W 等快捷键目前只在应用浏览器工具栏获得焦点时生效，不接管原生网页内部的按键。文件产出预览仍由「文件」页签拥有，终端与代码审查不属于本功能。

代码入口：`apps/desktop/src-tauri/src/embedded_browser.rs`、`apps/web/src/api/browser.ts`、`apps/web/src/components/EmbeddedBrowser.tsx`。原生导航／标签／隔离的可重复验收脚本为 `scripts/smoke-browser.mjs`：仅在本地桌面开发进程临时设置 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=9223` 后运行 `node scripts/smoke-browser.mjs`。脚本使用本地临时 HTTP 夹具（包含拒绝 iframe 的响应头），测试完成会关闭自己创建的标签与夹具端口；调试端口不写入应用配置，验收后退出调试进程。测试使用的 profile 可在 debug 构建通过 `OPC_BROWSER_DATA_DIR` 指向独立临时目录。

不启用调试端口时，可运行 `node scripts/smoke-browser.mjs --serve-fixtures`，在桌面地址栏打开输出的临时网址，人工验证导航、多标签和布局；结束后用 Ctrl+C 停止夹具。2026-09-17 已在当前 Windows 桌面实际验证拒绝 iframe 的网页渲染、前后退、刷新、普通 `_blank` 链接与 `window.open` 创建内嵌标签、工作台切换后恢复标签、面板收起／恢复、拖动调宽，以及新建任务弹窗打开时隐藏网页并在取消后恢复。该人工验收不等同于 CDP 脚本中的 IPC 隔离测试；后者尚未在本环境运行，不计入默认门禁。

Shell 插件通过 `apps/desktop/src-tauri/src/host_shell.rs` 保留原生进程管理、生命周期、退出清理和命令委派，但不注入其全局 JavaScript 链接拦截器。原拦截器会取消所有 `_blank` 链接并尝试 Shell IPC，在无应用权限的浏览器标签中导致点击无响应。新窗口改由原生 WebView2 事件统一管理；未放宽浏览器的 Shell 权限。

### 会话与日志

- 会话令牌每个 generation 重新生成；动态端口每代通过端口 `0` 重新申请，端口值允许被 OS 复用。两者都只存在于进程内。
- 基础地址只能是 http://127.0.0.1:非零端口。
- 浏览器请求要求允许的 Origin 和 Bearer Token。
- WebView 每次请求生成 UUID v4；Sidecar 将规范 UUID 写入 `X-Request-ID` 响应头、统一错误体和脱敏访问日志，前端网络/超时错误也保留本次生成值。Tauri 生命周期日志保持独立白名单事件。
- Sidecar 离开 ready 时前端立即清除缓存连接并取消/清空 TanStack Query；新 generation ready 后重新获取。ready generation 变化还会补偿漏过中间状态的轮询。
- 数据库路径和 Artifact root 由桌面层在每次启动时注入；WebView 不持有内部 `objects/<artifact-id>` 路径，也不能绕过 Sidecar 读取或删除文件。Sidecar HTTP read/write timeout 为 180 秒；Task 文件上传和下载采用 120 秒客户端端到端超时，普通小型 JSON API 继续使用较短超时。

## 与其他模块协作

- [数据管理](data-management.md)：启动迁移、恢复页、文件对话框、更新前备份和数据保留。
- [设置](settings.md)：展示平台能力、权限、托盘、自启、快捷键、诊断和离线更新入口。
- [命令与搜索](command-search.md)：OS 全局快捷键显示窗口并打开命令面板；服务状态控制业务搜索可用性。
- [专注](focus.md)：托盘控制、原生通知、退出前 Session 处理和系统勿扰引导。
- [收件箱](inbox.md)：通知点击打开资源；系统维护故障生成本地待办。
- [本地 Agent](local-agents.md)：受控进程、文件授权、沙箱、取消和退出清理。
- [任务](tasks.md) 与后续业务模块：桌面层只提供系统能力，不绕过其 API 与状态机。

## 分阶段实施

### v0.1-A：Sidecar 可靠性

- 已扩展前端全局服务状态：桌面 `starting/restarting/ready/error + generation`、非 ready 连接/Query 清理、generation 补偿和浏览器降级。
- 已实现启动失败恢复页 v1、内置 Sidecar 两次有界自动重启、500 ms/2 s 退避、连续 Ready 30 秒预算重置，以及每代新 token/动态端口申请。
- 已实现数据库父目录运行锁、父管道 EOF 优雅退出、安全应用重启门禁和并发 shutdown 共享 stop；孤儿/进程树治理仍待实现。
- 补真实 Tauri 与 Sidecar 父崩溃、进程树、三平台和安装包集成测试。

### v0.1-B：日志与维护

- 已接通 OPC_LOG_DIR 的启动故障 journal、原子更新、损坏隔离和 ready 前补偿，以及 Go Sidecar/Tauri 壳脱敏日志、5 MiB/3 归档轮转、敏感信息排除和文件故障降级；WebView→Sidecar request ID 已完成。
- 诊断页、脱敏摘要、诊断包 v1、无参数 `open_log_directory`、Tauri 壳自身日志和 API request ID 关联已完成。
- 数据库启动/迁移、Sidecar 启动、备份恢复、运行期数据库操作失败和 1–100 GiB 可配置低空间已接 maintenance 状态；物理卷同卷去重、无路径手动容量检查和 30 天本地容量趋势已在设置页交付，卷身份不离开 Sidecar 进程或 API。

### v0.1-C：系统集成

- 托盘最小源码闭环已接：显示/隐藏/退出、固定动作白名单、fallback 图标和不可用安全降级；原生链接/实机交互、设置开关、业务动作和状态图标仍待。
- 继续逐项实现原生通知、其他 OS 全局快捷键、文件对话框和开机启动。
- 每项能力使用独立最小权限、平台检测和降级说明。
- 完成窗口关闭/隐藏/退出语义和专注状态显示。

### v0.1-D：离线更新与发布

- 定义签名离线包、兼容矩阵、验签、更新前备份和失败回退。
- 建立多平台 Sidecar 与 Tauri 构建 CI。
- 完成签名、公证、安装包、性能和干净机矩阵。

### 后续评审

- 在线 Updater 若未来进入范围，必须单独 ADR 和用户开关；当前实现、设置和权限中保持关闭。
- 新平台、远程连接或云同步需要独立安全与数据边界评审。

## 验收状态

### 当前已验证

- [x] 生产 Sidecar 配置只接受 127.0.0.1，动态端口、会话令牌与精确 Origin 契约已有测试。
- [x] single-instance 只聚焦现有窗口，不有意启动第二个桌面 Sidecar。
- [x] ready 地址为非 loopback、端口 0、带凭据或路径时被拒绝。
- [x] Tauri 创建 Artifact root 并通过 `OPC_ARTIFACT_DIR` 传给 Sidecar；开发脚本使用独立开发 root。
- [x] Sidecar 在 ready 前验证数据库绑定 marker/目录、获取 root 进程级独占锁并协调 staging/objects/trash/quarantine；错库或第二 Sidecar 共用 root 时启动失败，文件读写只经过受控 API。
- [x] 数据库父目录固定 `.opc-sidecar-run.lock` 在 pending restore、迁移和 DB open 前取得 OS 独占锁；冲突立即失败且不触碰数据库。
- [x] 内置 Sidecar 最多按 500 ms、2 s 自动重启两次，当前 generation 连续 Ready 30 秒重置预算；只有真实 `Terminated` 才为已启动代际重拉，外部/shutdown/无 Terminated 流关闭均不触发。
- [x] 正常退出发送 shutdown，等待 drain/WAL checkpoint，超时只终止精确 child generation；并发调用共享一次 stop，ready 超时竞态不会伪造 exited，父管道 EOF 由 `OPC_EXIT_ON_STDIN_CLOSE=true` 触发 Go 优雅关闭。
- [x] 恢复计划挂起后可从设置页请求安全重启；command 拒绝外部 Sidecar，受管 child 只接受 code 0/no signal，未创建 child 的 bundled 启动失败允许继续，延迟干净退出后可重试。
- [x] 当前源码门禁通过 Web 全量 90 个文件 / 607 项、Go `go test ./... -count=1` 与 `go vet ./...`、Sidecar 构建、Rust 格式和锁定 Cargo metadata；托盘与能力快照新增单元测试源码已完成静态复核，但受工具链限制未执行 Rust 测试或原生链接。
- [x] 在线 Updater 未启用，也不是启动依赖。

### 仍待验收

- [ ] 真实 Tauri/Sidecar 父进程崩溃、进程树、hard-hung orphan 与孙进程治理；当前运行锁只阻止第二个进程接触同库，不自动回收。
- [x] 启动前白名单 incident journal、稳定 ID 重放、损坏隔离及健康启动补偿。
- [x] 脱敏诊断包 v1（不含原始日志）。
- [x] Go Sidecar 脱敏日志落盘/轮转与 stderr 降级。
- [x] 设置运行诊断可通过无参数 Tauri command 打开自身 `appLogDir`；浏览器模式不伪造路径。
- [x] Tauri 壳白名单 JSONL 生命周期日志、5 MiB/3 归档、非普通目标拒绝与 stderr 降级。
- [x] WebView→Sidecar request ID：每次请求使用 UUID v4，响应头、错误体、前端错误和访问日志可关联；非法客户端值由 Sidecar 替换为规范 UUID。
- [x] 全局服务恢复页 v1：starting/restarting/error 拦截业务页，ready 自动放行；generation、查询清理、状态重查、脱敏日志入口、安全重启、版本白名单与原始错误排除。
- [x] 数据库打开前备份选择：恢复页 error 态可列出现有备份、二次确认后经 `prepare-restore` 安排 pending restore，跳过则不替换数据；实时恢复进度已通过白名单启动阶段交付。
- [ ] 托盘源码和关闭到托盘设置已完成最小闭环；当前 Windows x64 已完成 MSVC 链接、Rust 测试与安装包生成，仍需验证偏好启停、关闭/恢复/退出与 Sidecar 无残留，再补 macOS/Linux、运行状态和业务动作。原生通知、其他 OS 全局快捷键、开机启动和原生业务文件对话框仍待。
- [ ] 签名离线更新、迁移前验证备份与失败回退。
- [ ] Windows、macOS、Linux 对应签名/公证、干净机、备份恢复、更新和性能证据；当前仅有 Windows x64 未签名本地 NSIS/MSI 包。
- [x] 当前主机已补齐 MSVC `link.exe` 与 Windows SDK，`cargo check` / 38 项 `cargo test`、Tauri 链接以及 Windows x64 NSIS/MSI 安装包生成通过。

## 相关代码/PRD链接

- [PRD：技术架构方案](../opc-workspace-PRD.md#4-技术架构方案)
- [PRD：部署与分发](../opc-workspace-PRD.md#8-部署与分发)
- [PRD：T-02 Tauri 桌面壳与 Sidecar 生命周期](../opc-workspace-PRD.md#1042-t-02-tauri-桌面壳与-sidecar-生命周期)
- [PRD：MVP 技术验收标准](../opc-workspace-PRD.md#93-mvp-技术验收标准)
- [当前 Tauri 应用入口](../../apps/desktop/src-tauri/src/lib.rs)
- [当前 Sidecar 生命周期](../../apps/desktop/src-tauri/src/sidecar.rs)
- [当前 Tauri 配置](../../apps/desktop/src-tauri/tauri.conf.json)
- [当前最小 capability](../../apps/desktop/src-tauri/capabilities/default.json)
- [当前 Sidecar 进程入口](../../services/sidecar/cmd/server/main.go)
- [当前 Artifact store](../../services/sidecar/internal/api/artifact_store.go)
- [当前 API 安全中间件](../../services/sidecar/internal/api/middleware.go)
- [当前前端连接发现](../../apps/web/src/api/client.ts)
