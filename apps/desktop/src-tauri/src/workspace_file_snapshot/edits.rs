use super::*;
use base64::{Engine, engine::general_purpose::STANDARD};
use std::{
    collections::HashMap,
    fs,
    io::{Seek, SeekFrom, Write},
    sync::atomic::{AtomicBool, Ordering},
    time::{SystemTime, UNIX_EPOCH},
};
use tauri::Manager;

const MAX_RECORDS: usize = 64;
const MAX_RECORD_BYTES: usize = 4 * 1024 * 1024;

fn ensure_legacy_edits_disabled() -> Result<(), String> {
    Err("旧版原地文件恢复仍保持关闭；请使用替换记录的受控恢复流程".into())
}

#[cfg(all(test, windows))]
mod tests;

#[derive(Clone, Default)]
pub struct FileEditState {
    serial: Arc<Mutex<()>>,
    active: Arc<Mutex<HashMap<String, ActiveFileOperation>>>,
}

#[derive(Clone)]
struct ActiveFileOperation {
    root_id: String,
    cancelled: Arc<AtomicBool>,
}

struct ActiveFileOperationLease {
    state: FileEditState,
    operation_id: String,
    operation: ActiveFileOperation,
}

impl FileEditState {
    fn begin(&self, root_id: &str, operation_id: &str) -> Result<ActiveFileOperationLease, String> {
        if !uuid::Uuid::parse_str(operation_id).is_ok_and(|value| value.to_string() == operation_id)
        {
            return Err("文件操作身份无效".into());
        }
        let mut active = self.active.lock().map_err(|_| "文件操作状态不可用")?;
        if !active.is_empty() {
            return Err("已有文件操作正在等待确认或执行，请先完成或取消".into());
        }
        let operation = ActiveFileOperation {
            root_id: root_id.into(),
            cancelled: Arc::new(AtomicBool::new(false)),
        };
        active.insert(operation_id.into(), operation.clone());
        Ok(ActiveFileOperationLease {
            state: self.clone(),
            operation_id: operation_id.into(),
            operation,
        })
    }

    fn cancel(&self, root_id: &str, operation_id: &str) -> Result<(), String> {
        let active = self.active.lock().map_err(|_| "文件操作状态不可用")?;
        if let Some(operation) = active.get(operation_id) {
            if operation.root_id != root_id {
                return Err("文件操作不属于当前目录选择".into());
            }
            operation.cancelled.store(true, Ordering::Release);
        }
        Ok(())
    }
}

impl ActiveFileOperationLease {
    #[cfg(windows)]
    fn scope(
        &self,
        app: tauri::AppHandle,
        root: PathBuf,
    ) -> crate::file_helper_image::launch::CurrentScope {
        let state = self.state.clone();
        let operation_id = self.operation_id.clone();
        let expected_root_id = self.operation.root_id.clone();
        let cancelled = self.operation.cancelled.clone();
        Arc::new(move |root_id| {
            if root_id != expected_root_id
                || cancelled.load(Ordering::Acquire)
                || !state
                    .active
                    .lock()
                    .map_err(|_| "文件操作状态不可用")?
                    .get(&operation_id)
                    .is_some_and(|current| {
                        current.root_id == expected_root_id
                            && Arc::ptr_eq(&current.cancelled, &cancelled)
                    })
            {
                return Err("本次文件确认已取消或范围已失效".into());
            }
            if app.state::<WorkspaceState>().root(root_id)? != root {
                return Err("本次工作目录选择已变化".into());
            }
            Ok(())
        })
    }
}

impl Drop for ActiveFileOperationLease {
    fn drop(&mut self) {
        if let Ok(mut active) = self.state.active.lock() {
            if active
                .get(&self.operation_id)
                .is_some_and(|current| Arc::ptr_eq(&current.cancelled, &self.operation.cancelled))
            {
                active.remove(&self.operation_id);
            }
        }
    }
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FileWriteCapability {
    available: bool,
    reason: String,
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Record {
    version: u32,
    id: String,
    root: PathBuf,
    path: String,
    root_identity: FileIdentity,
    file_identity: FileIdentity,
    before: Vec<u8>,
    after: Vec<u8>,
    created_at: u64,
    restores: Option<String>,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct StoredRecord {
    record: Record,
    digest: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FileEditResult {
    id: String,
    // applied is a verified local write, not a model/tool/business receipt.
    status: String,
    backup_path: String,
}
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FileRecoveryItem {
    id: String,
    path: String,
    created_at: Option<u64>,
    restores: Option<String>,
    version: u32,
}
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FileRecoveryList {
    records: Vec<FileRecoveryItem>,
    damaged: usize,
    directory: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    replacement_directory: Option<String>,
}
#[derive(Debug, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum ReplacementFileState {
    Missing,
    Original,
    Candidate,
    Changed,
    Foreign,
    Unavailable,
}
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FileReplacementPreview {
    pub(super) id: String,
    pub(super) path: String,
    pub(super) observation: String,
    pub(super) target: ReplacementFileState,
    pub(super) parked: ReplacementFileState,
    pub(super) staged: ReplacementFileState,
    pub(super) current_base64: Option<String>,
    pub(super) original_base64: String,
    pub(super) candidate_base64: String,
}
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FileReplacementReview {
    #[serde(flatten)]
    pub(super) preview: FileReplacementPreview,
    pub(super) review_id: String,
    pub(super) mode: String,
    pub(super) expires_in_seconds: u64,
}
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FileRecoveryPreview {
    id: String,
    snapshot_id: String,
    path: String,
    current_base64: String,
    original_base64: String,
    expires_in_seconds: u64,
}

fn valid_id(id: &str) -> bool {
    uuid::Uuid::parse_str(id).is_ok_and(|v| v.to_string() == id)
}
fn record_path(storage: &Path, id: &str) -> Result<PathBuf, String> {
    if !valid_id(id) {
        return Err("恢复记录身份无效".into());
    }
    Ok(storage.join(format!("{id}.json")))
}
fn storage_path(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    app.path()
        .app_local_data_dir()
        .map(|p| p.join("workspace-file-recovery-v1"))
        .map_err(|_| "应用本地恢复目录不可用".into())
}
fn replacement_storage_path(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    app.path()
        .app_local_data_dir()
        .map(|p| p.join("workspace-file-recovery-v2"))
        .map_err(|_| "应用本地恢复目录不可用".into())
}

#[cfg(windows)]
fn load_record(storage: &Path, id: &str) -> Result<Record, String> {
    let _pin = windows::PinnedDirectory::open(storage, false)?;
    let mut file = windows::journal_file(&record_path(storage, id)?, false)?;
    let (json, _, _) = windows::read_regular(&mut file, MAX_RECORD_BYTES)?;
    let stored: StoredRecord =
        serde_json::from_slice(&json).map_err(|_| "恢复记录损坏，不能自动恢复")?;
    let record = stored.record;
    let encoded = serde_json::to_string(&record).map_err(|_| "恢复记录无效")?;
    if record.version != 1
        || record.id != id
        || hash(&encoded) != stored.digest
        || record.before.len() > MAX_BYTES
        || record.after.len() > MAX_BYTES
        || record.before == record.after
        || !record.root.is_absolute()
        || record.restores.as_ref().is_some_and(|id| !valid_id(id))
    {
        return Err("恢复记录完整性检查失败".into());
    }
    validate_path(&record.path)?;
    Ok(record)
}

#[cfg(windows)]
fn list(storage: &Path, root: &Path) -> Result<FileRecoveryList, String> {
    let mut result = FileRecoveryList {
        records: Vec::new(),
        damaged: 0,
        directory: storage.display().to_string(),
        replacement_directory: None,
    };
    if !storage.try_exists().map_err(|_| "恢复目录状态不可用")? {
        return Ok(result);
    }
    let _pin = windows::PinnedDirectory::open(storage, false)?;
    for (index, entry) in fs::read_dir(storage)
        .map_err(|_| "无法读取恢复目录")?
        .take(MAX_RECORDS + 1)
        .enumerate()
    {
        if index == MAX_RECORDS {
            return Err("恢复目录超过 64 项，不能展示不完整列表；请先备份并人工整理记录".into());
        }
        let entry = entry.map_err(|_| "无法读取恢复记录")?;
        let name = entry.file_name().to_string_lossy().into_owned();
        let Some(id) = name.strip_suffix(".json").filter(|id| valid_id(id)) else {
            result.damaged += 1;
            continue;
        };
        match load_record(storage, id) {
            Ok(record) if record.root == root => result.records.push(FileRecoveryItem {
                id: record.id,
                path: record.path,
                created_at: Some(record.created_at),
                restores: record.restores,
                version: 1,
            }),
            Ok(_) => (),
            Err(_) => result.damaged += 1,
        }
    }
    result
        .records
        .sort_by(|a, b| b.created_at.cmp(&a.created_at).then(a.id.cmp(&b.id)));
    Ok(result)
}

#[cfg(windows)]
pub(super) fn list_all(
    storage: &Path,
    replacements: &Path,
    root: &Path,
) -> Result<FileRecoveryList, String> {
    let mut result = list(storage, root)?;
    result.replacement_directory = Some(replacements.display().to_string());
    if !replacements
        .try_exists()
        .map_err(|_| "替换记录目录状态不可用")?
    {
        return Ok(result);
    }
    let _pin = windows::PinnedDirectory::open(replacements, false)?;
    for (index, entry) in fs::read_dir(replacements)
        .map_err(|_| "无法读取替换记录目录")?
        .take(MAX_RECORDS + 1)
        .enumerate()
    {
        if index == MAX_RECORDS {
            return Err("替换记录超过 64 项，不能展示不完整列表；请先备份并人工整理".into());
        }
        let entry = entry.map_err(|_| "无法完整读取替换记录")?;
        let name = entry.file_name().to_string_lossy().into_owned();
        let Some(id) = name.strip_suffix(".json").filter(|id| valid_id(id)) else {
            result.damaged += 1;
            continue;
        };
        match super::replacement_journal::load(replacements, id) {
            Ok(intent) if intent.root == root => result.records.push(FileRecoveryItem {
                id: intent.id,
                path: intent.path,
                created_at: None,
                restores: None,
                version: 2,
            }),
            Ok(_) => (),
            Err(_) => result.damaged += 1,
        }
    }
    Ok(result)
}

#[cfg(windows)]
fn persist_record(
    storage: &Path,
    record: &Record,
) -> Result<(fs::File, windows::PinnedDirectory), String> {
    let _pin = windows::PinnedDirectory::open(storage, true)?;
    if fs::read_dir(storage)
        .map_err(|_| "恢复目录不可用")?
        .take(MAX_RECORDS)
        .count()
        >= MAX_RECORDS
    {
        return Err("恢复目录已满（64 项）；请先备份并人工整理记录，未写入目标文件".into());
    }
    let encoded = serde_json::to_string(record).map_err(|_| "无法编码恢复记录")?;
    let stored = serde_json::json!({ "record": record, "digest": hash(&encoded) });
    let bytes = serde_json::to_vec(&stored).map_err(|_| "无法编码恢复记录")?;
    if bytes.len() > MAX_RECORD_BYTES {
        return Err("恢复记录过大，未写入目标文件".into());
    }
    let mut file = windows::journal_file(&record_path(storage, &record.id)?, true)?;
    // An incomplete journal is never used for a write. Keep damaged records
    // visible rather than deleting evidence; there is no target mutation yet.
    file.write_all(&bytes)
        .and_then(|_| file.sync_all())
        .map_err(|_| "恢复备份未能刷盘，未写入目标文件；请检查恢复目录")?;
    Ok((file, _pin))
}

#[cfg(windows)]
fn apply(
    snapshots: &FileSnapshotState,
    storage: &Path,
    root_id: &str,
    root: &Path,
    snapshot_id: &str,
    operation_id: &str,
    content: &[u8],
    restores: Option<String>,
    fault: Option<usize>,
) -> Result<FileEditResult, String> {
    let location = record_path(storage, operation_id)?;
    if content.len() > MAX_BYTES
        || (restores.is_none()
            && (content.len() > 32 * 1024
                || content.contains(&0)
                || std::str::from_utf8(content).is_err()))
    {
        return Err("候选全文超限或包含二进制内容".into());
    }
    if location.try_exists().map_err(|_| "恢复记录状态不可用")? {
        let existing = load_record(storage, operation_id)?;
        if existing.root != root || existing.after != content || existing.restores != restores {
            return Err("操作身份已被其他修改占用".into());
        }
        return Ok(FileEditResult {
            id: operation_id.into(),
            status: "already_recorded".into(),
            backup_path: location.display().to_string(),
        });
    }
    let snapshot = {
        let mut values = snapshots.0.lock().map_err(|_| "文件快照状态不可用")?;
        values.retain(|_, s| s.created.elapsed() < TTL);
        let snapshot = values
            .get(snapshot_id)
            .ok_or("文件基线已过期或已使用，请重新读取并审查")?;
        if snapshot.root_id != root_id || snapshot.root != root {
            return Err("文件基线不属于当前目录".into());
        }
        match (&restores, &snapshot.recovery) {
            (None, None) => (),
            (Some(id), Some((reviewed_id, reviewed_bytes)))
                if id == reviewed_id && content == reviewed_bytes =>
            {
                ()
            }
            _ => return Err("恢复来源与已审查内容不一致，未写入".into()),
        }
        if snapshot.file.content == content {
            return Err("候选内容与原文件相同，无需写入".into());
        }
        // Consume before I/O. Failed/uncertain attempts never silently retry.
        values.remove(snapshot_id).ok_or("文件基线不可用")?
    };
    let target = root.join(&snapshot.path);
    let path_key = |path: &Path| {
        path.to_string_lossy()
            .replace('/', "\\")
            .trim_start_matches("\\\\?\\")
            .to_lowercase()
    };
    let target_key = path_key(&target);
    let storage_key = path_key(storage);
    if target_key.starts_with(&(storage_key + "\\")) {
        return Err("不能修改应用恢复记录".into());
    }
    let mut held = windows::open(root, &snapshot.path, true)?;
    let baseline = held.read()?;
    if baseline != snapshot.file {
        return Err("文件或目录已变化，未写入；请重新读取并审查".into());
    }
    if let Some(id) = &restores {
        let origin = load_record(storage, id)?;
        if origin.root != root
            || origin.path != snapshot.path
            || origin.root_identity != snapshot.file.root_identity
            || origin.file_identity != snapshot.file.file_identity
            || origin.before != content
        {
            return Err("恢复来源或目标身份已变化，未写入".into());
        }
    }
    let record = Record {
        version: 1,
        id: operation_id.into(),
        root: root.into(),
        path: snapshot.path,
        root_identity: snapshot.file.root_identity,
        file_identity: snapshot.file.file_identity,
        before: snapshot.file.content,
        after: content.into(),
        created_at: SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_err(|_| "系统时间不可用")?
            .as_secs(),
        restores,
    };
    let _journal_guard = persist_record(storage, &record)?;
    // Detect late observed links/changes too. This is NOT sufficient to close
    // the link-to-write race, hence the independent production gate above.
    if held.read()? != baseline {
        return Err("备份期间文件或链接状态变化，未写入目标文件".into());
    }
    // The source handle remains exclusive and ancestors pinned throughout
    // compare, durable journal, write, truncate, flush and exact readback.
    let written = (|| -> Result<(), String> {
        held.file
            .seek(SeekFrom::Start(0))
            .map_err(|_| "写入定位失败")?;
        if let Some(count) = fault {
            held.file
                .write_all(&content[..count.min(content.len())])
                .map_err(|_| "写入失败")?;
            return Err("测试注入中断".into());
        }
        held.file
            .write_all(content)
            .and_then(|_| held.file.set_len(content.len() as u64))
            .and_then(|_| held.file.sync_all())
            .map_err(|_| "写入或刷盘失败")?;
        if held.read()?.content != content {
            return Err("写入后完整核验失败".into());
        }
        Ok(())
    })();
    Ok(FileEditResult {
        id: operation_id.into(),
        status: if written.is_ok() {
            "applied"
        } else {
            "recovery_required"
        }
        .into(),
        backup_path: location.display().to_string(),
    })
}

#[cfg(windows)]
fn preview(
    snapshots: &FileSnapshotState,
    storage: &Path,
    root_id: &str,
    root: &Path,
    id: &str,
) -> Result<FileRecoveryPreview, String> {
    let record = load_record(storage, id)?;
    if record.root != root {
        return Err("恢复记录不属于当前工作目录".into());
    }
    // Recovery must also handle a torn UTF-8 write. Capture exact raw bytes,
    // never replacement characters. This snapshot is not an AI disclosure.
    let mut values = snapshots.0.lock().map_err(|_| "文件快照状态不可用")?;
    values.retain(|_, s| s.created.elapsed() < TTL);
    if values.len() >= MAX_SNAPSHOTS {
        return Err("请先释放旧文件基线".into());
    }
    let current = read_exact(root, &record.path)?;
    if current.root_identity != record.root_identity
        || current.file_identity != record.file_identity
    {
        return Err("目录或文件已替换，不能自动恢复；原文仍保存在恢复记录中".into());
    }
    let snapshot_id = uuid::Uuid::new_v4().to_string();
    let result = FileRecoveryPreview {
        id: id.into(),
        snapshot_id: snapshot_id.clone(),
        path: record.path.clone(),
        current_base64: STANDARD.encode(&current.content),
        original_base64: STANDARD.encode(&record.before),
        expires_in_seconds: TTL.as_secs(),
    };
    values.insert(
        snapshot_id,
        Snapshot {
            root_id: root_id.into(),
            root: root.into(),
            path: record.path,
            file: current,
            created: Instant::now(),
            recovery: Some((id.into(), record.before)),
        },
    );
    Ok(result)
}

#[tauri::command]
pub async fn workspace_file_write_capability() -> Result<FileWriteCapability, String> {
    tauri::async_runtime::spawn_blocking(|| {
        #[cfg(windows)]
        let available = crate::file_helper_image::product_ready();
        #[cfg(not(windows))]
        let available = false;
        FileWriteCapability {
            available,
            reason: if available {
                "每次仅处理当前审查的一份文件，并经过原生确认与 Windows 系统授权".into()
            } else {
                "当前桌面构建未提供已绑定的单文件安全辅助程序；仍可只读审查差异".into()
            },
        }
    })
    .await
    .map_err(|_| "文件写入能力检查失败".into())
}

#[tauri::command]
pub fn workspace_file_operation_cancel(
    edits: State<'_, FileEditState>,
    root_id: String,
    operation_id: String,
) -> Result<(), String> {
    edits.cancel(&root_id, &operation_id)
}

#[cfg(windows)]
fn run_reviewed_operation(
    app: tauri::AppHandle,
    lease: &ActiveFileOperationLease,
    root: &Path,
    request: crate::file_operation_contract::CheckedRequest,
) -> Result<Option<()>, String> {
    if !crate::file_helper_image::product_ready() {
        return Err("当前桌面构建未绑定可核验的单文件安全辅助程序".into());
    }
    let scope = lease.scope(app, root.to_path_buf());
    crate::file_helper_image::launch::review_and_exchange(request, scope).map_err(|error| {
        // The helper may already have crossed `started`; never collapse a
        // transport or process error into a safe-to-retry rejection.
        format!("文件操作没有形成可确认的产品回执；请核对恢复记录且不要重复提交：{error}")
    })
}

#[tauri::command]
pub async fn workspace_file_edit_apply(
    app: tauri::AppHandle,
    state: State<'_, WorkspaceState>,
    snapshots: State<'_, FileSnapshotState>,
    edits: State<'_, FileEditState>,
    root_id: String,
    snapshot_id: String,
    operation_id: String,
    content: String,
) -> Result<FileEditResult, String> {
    let root = state.root(&root_id)?;
    let storage = replacement_storage_path(&app)?;
    let snapshots = snapshots.inner().clone();
    let edits = edits.inner().clone();
    let lease = edits.begin(&root_id, &operation_id)?;
    tauri::async_runtime::spawn_blocking(move || {
        let _guard = edits.serial.lock().map_err(|_| "文件写入状态不可用")?;
        #[cfg(windows)]
        {
            if !crate::file_helper_image::product_ready() {
                return Err("当前桌面构建未绑定可核验的单文件安全辅助程序".into());
            }
            let request = snapshots.freeze_edit_request(
                &root_id,
                &root,
                &snapshot_id,
                &operation_id,
                content.as_bytes(),
            )?;
            let outcome = run_reviewed_operation(app, &lease, &root, request)?;
            Ok(FileEditResult {
                id: operation_id.clone(),
                status: if outcome.is_some() {
                    "applied"
                } else {
                    "cancelled"
                }
                .into(),
                backup_path: if outcome.is_some() {
                    storage
                        .join(format!("{operation_id}.json"))
                        .display()
                        .to_string()
                } else {
                    String::new()
                },
            })
        }
        #[cfg(not(windows))]
        {
            let _ = (
                snapshots,
                storage,
                root_id,
                root,
                snapshot_id,
                operation_id,
                content,
            );
            Err("文件写入目前只支持 Windows 桌面端".into())
        }
    })
    .await
    .map_err(|_| "写入结果未确定，请在文件恢复记录中核对，不要重复写入")?
}

#[tauri::command]
pub async fn workspace_file_replacement_apply(
    app: tauri::AppHandle,
    state: State<'_, WorkspaceState>,
    reviews: State<'_, FileRecoveryReviewState>,
    edits: State<'_, FileEditState>,
    root_id: String,
    review_id: String,
    operation_id: String,
) -> Result<FileEditResult, String> {
    let root = state.root(&root_id)?;
    let storage = replacement_storage_path(&app)?;
    let reviews = reviews.inner().clone();
    let edits = edits.inner().clone();
    let lease = edits.begin(&root_id, &operation_id)?;
    tauri::async_runtime::spawn_blocking(move || {
        let _guard = edits.serial.lock().map_err(|_| "文件恢复状态不可用")?;
        #[cfg(windows)]
        {
            if !crate::file_helper_image::product_ready() {
                return Err("当前桌面构建未绑定可核验的单文件安全辅助程序".into());
            }
            let request = reviews.freeze_request(&root_id, &root, &review_id, &operation_id)?;
            let record_id = match &request.facts().operation {
                crate::file_operation_contract::Operation::RestoreMissing { record_id, .. }
                | crate::file_operation_contract::Operation::UndoInstalled { record_id, .. } => {
                    record_id.clone()
                }
                _ => return Err("恢复审查没有固定恢复记录".into()),
            };
            let outcome = run_reviewed_operation(app, &lease, &root, request)?;
            Ok(FileEditResult {
                id: operation_id,
                status: if outcome.is_some() {
                    "applied"
                } else {
                    "cancelled"
                }
                .into(),
                backup_path: if outcome.is_some() {
                    storage
                        .join(format!("{record_id}.json"))
                        .display()
                        .to_string()
                } else {
                    String::new()
                },
            })
        }
        #[cfg(not(windows))]
        {
            let _ = (
                app,
                reviews,
                edits,
                lease,
                storage,
                root_id,
                root,
                review_id,
                operation_id,
            );
            Err("受控文件恢复目前只支持 Windows 桌面端".into())
        }
    })
    .await
    .map_err(|_| String::from("恢复结果未确定，请核对恢复记录且不要重复操作"))?
}

#[tauri::command]
pub async fn workspace_file_recovery_list(
    app: tauri::AppHandle,
    state: State<'_, WorkspaceState>,
    edits: State<'_, FileEditState>,
    root_id: String,
) -> Result<FileRecoveryList, String> {
    let root = state.root(&root_id)?;
    let storage = storage_path(&app)?;
    let replacements = replacement_storage_path(&app)?;
    let edits = edits.inner().clone();
    tauri::async_runtime::spawn_blocking(move || {
        let _guard = edits.serial.lock().map_err(|_| "文件恢复状态不可用")?;
        #[cfg(windows)]
        {
            list_all(&storage, &replacements, &root)
        }
        #[cfg(not(windows))]
        {
            let _ = (storage, replacements, root);
            Err("文件恢复目前只支持 Windows 桌面端".into())
        }
    })
    .await
    .map_err(|_| "恢复记录读取失败")?
}

#[tauri::command]
pub async fn workspace_file_replacement_preview(
    app: tauri::AppHandle,
    state: State<'_, WorkspaceState>,
    edits: State<'_, FileEditState>,
    root_id: String,
    record_id: String,
) -> Result<FileReplacementPreview, String> {
    let root = state.root(&root_id)?;
    let storage = replacement_storage_path(&app)?;
    let edits = edits.inner().clone();
    tauri::async_runtime::spawn_blocking(move || {
        let _guard = edits.serial.lock().map_err(|_| "文件恢复状态不可用")?;
        #[cfg(windows)]
        {
            super::replacement_journal::preview(&storage, &record_id, &root)
        }
        #[cfg(not(windows))]
        {
            let _ = (storage, record_id, root);
            Err("替换记录只支持 Windows 桌面端".into())
        }
    })
    .await
    .map_err(|_| "替换记录核对失败")?
}

#[tauri::command]
pub async fn workspace_file_replacement_review(
    app: tauri::AppHandle,
    state: State<'_, WorkspaceState>,
    reviews: State<'_, FileRecoveryReviewState>,
    root_id: String,
    record_id: String,
    mode: String,
) -> Result<FileReplacementReview, String> {
    let root = state.root(&root_id)?;
    let storage = replacement_storage_path(&app)?;
    let reviews = reviews.inner().clone();
    tauri::async_runtime::spawn_blocking(move || {
        #[cfg(windows)]
        {
            reviews.capture(&storage, &root_id, &root, &record_id, &mode)
        }
        #[cfg(not(windows))]
        {
            let _ = (reviews, storage, root_id, root, record_id, mode);
            Err("恢复审查只支持 Windows 桌面端".into())
        }
    })
    .await
    .map_err(|_| "恢复条件复核失败")?
}

#[tauri::command]
pub fn workspace_file_replacement_review_release(
    reviews: State<'_, FileRecoveryReviewState>,
    root_id: String,
    review_id: String,
) -> Result<(), String> {
    #[cfg(windows)]
    {
        reviews.release(&root_id, &review_id)
    }
    #[cfg(not(windows))]
    {
        let _ = (reviews, root_id, review_id);
        Ok(())
    }
}

#[tauri::command]
pub async fn workspace_file_recovery_preview(
    app: tauri::AppHandle,
    state: State<'_, WorkspaceState>,
    snapshots: State<'_, FileSnapshotState>,
    edits: State<'_, FileEditState>,
    root_id: String,
    record_id: String,
) -> Result<FileRecoveryPreview, String> {
    let root = state.root(&root_id)?;
    let storage = storage_path(&app)?;
    let snapshots = snapshots.inner().clone();
    let edits = edits.inner().clone();
    tauri::async_runtime::spawn_blocking(move || {
        let _guard = edits.serial.lock().map_err(|_| "文件恢复状态不可用")?;
        #[cfg(windows)]
        {
            preview(&snapshots, &storage, &root_id, &root, &record_id)
        }
        #[cfg(not(windows))]
        {
            let _ = (snapshots, storage, root_id, root, record_id);
            Err("文件恢复目前只支持 Windows 桌面端".into())
        }
    })
    .await
    .map_err(|_| "恢复预览读取失败")?
}

#[tauri::command]
pub async fn workspace_file_recovery_apply(
    app: tauri::AppHandle,
    state: State<'_, WorkspaceState>,
    snapshots: State<'_, FileSnapshotState>,
    edits: State<'_, FileEditState>,
    root_id: String,
    record_id: String,
    snapshot_id: String,
    operation_id: String,
) -> Result<FileEditResult, String> {
    ensure_legacy_edits_disabled()?;
    let root = state.root(&root_id)?;
    let storage = storage_path(&app)?;
    let snapshots = snapshots.inner().clone();
    let edits = edits.inner().clone();
    tauri::async_runtime::spawn_blocking(move || {
        let _guard = edits.serial.lock().map_err(|_| "文件恢复状态不可用")?;
        #[cfg(windows)]
        {
            let record = load_record(&storage, &record_id)?;
            apply(
                &snapshots,
                &storage,
                &root_id,
                &root,
                &snapshot_id,
                &operation_id,
                &record.before,
                Some(record_id),
                None,
            )
        }
        #[cfg(not(windows))]
        {
            let _ = (
                snapshots,
                storage,
                root_id,
                root,
                record_id,
                snapshot_id,
                operation_id,
            );
            Err("文件恢复目前只支持 Windows 桌面端".into())
        }
    })
    .await
    .map_err(|_| "恢复结果未确定，请重新读取恢复记录，不要重复写入")?
}
