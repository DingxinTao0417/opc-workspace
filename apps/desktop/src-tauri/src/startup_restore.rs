use std::fs;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager};
use tauri_plugin_shell::ShellExt;
use uuid::Uuid;

const MAX_CHOICES: usize = 20;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct StartupRestoreChoice {
    pub id: String,
    pub created_at: Option<String>,
    pub verification_status: String,
    pub kind: String,
    pub schema_version: i64,
    pub note: Option<String>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct ScheduledRestoreResult {
    pub backup_id: String,
    pub rollback_backup_id: String,
    pub requested_at: String,
    pub restart_required: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PrepareRestoreArgs {
    pub backup_id: String,
    pub confirm: bool,
}

#[derive(Debug, Deserialize)]
struct ManifestFile {
    #[allow(dead_code)]
    path: String,
}

#[derive(Debug, Deserialize)]
struct BackupManifest {
    format_version: i64,
    id: String,
    created_at: String,
    #[serde(default)]
    verified_at: String,
    #[serde(default)]
    note: String,
    #[serde(default)]
    kind: String,
    schema_version: i64,
    #[serde(default)]
    database: Option<ManifestFile>,
    #[serde(default)]
    artifact_marker: Option<ManifestFile>,
}

fn backup_root(app: &AppHandle) -> Result<PathBuf, String> {
    let app_data_dir = app
        .path()
        .app_data_dir()
        .map_err(|_| "无法定位应用数据目录".to_owned())?;
    Ok(app_data_dir.join("backups"))
}

fn is_canonical_uuid(value: &str) -> bool {
    if value.is_empty() || value != value.to_lowercase() {
        return false;
    }
    match Uuid::parse_str(value) {
        Ok(parsed) => parsed.to_string() == value,
        Err(_) => false,
    }
}

fn read_choice(package_path: &Path, id: &str) -> StartupRestoreChoice {
    let manifest_path = package_path.join("manifest.json");
    let raw = match fs::read(&manifest_path) {
        Ok(raw) if raw.len() <= 1024 * 1024 => raw,
        _ => {
            return StartupRestoreChoice {
                id: id.to_owned(),
                created_at: None,
                verification_status: "invalid".to_owned(),
                kind: "unknown".to_owned(),
                schema_version: 0,
                note: None,
            };
        }
    };
    let manifest: BackupManifest = match serde_json::from_slice(&raw) {
        Ok(manifest) => manifest,
        Err(_) => {
            return StartupRestoreChoice {
                id: id.to_owned(),
                created_at: None,
                verification_status: "invalid".to_owned(),
                kind: "unknown".to_owned(),
                schema_version: 0,
                note: None,
            };
        }
    };
    if manifest.format_version != 1
        || manifest.id.to_lowercase() != id
        || manifest.schema_version < 1
        || manifest.database.is_none()
        || manifest.artifact_marker.is_none()
    {
        return StartupRestoreChoice {
            id: id.to_owned(),
            created_at: Some(manifest.created_at).filter(|value| !value.is_empty()),
            verification_status: "invalid".to_owned(),
            kind: "unknown".to_owned(),
            schema_version: manifest.schema_version,
            note: None,
        };
    }
    let status = if manifest.verified_at.is_empty() {
        "unverified"
    } else {
        "verified"
    };
    let mut note = manifest.note.trim().to_owned();
    if note.chars().count() > 120 {
        note = note.chars().take(120).collect();
    }
    StartupRestoreChoice {
        id: id.to_owned(),
        created_at: Some(manifest.created_at),
        verification_status: status.to_owned(),
        kind: if manifest.kind.is_empty() {
            "manual".to_owned()
        } else {
            manifest.kind
        },
        schema_version: manifest.schema_version,
        note: if note.is_empty() { None } else { Some(note) },
    }
}

/// Lists backup packages for the startup recovery picker. Responses never
/// include filesystem paths or raw manifest errors.
#[tauri::command]
pub fn list_startup_restore_choices(app: AppHandle) -> Result<Vec<StartupRestoreChoice>, String> {
    let root = backup_root(&app)?;
    let entries = match fs::read_dir(&root) {
        Ok(entries) => entries,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
            return Ok(Vec::new());
        }
        Err(_) => return Err("无法读取本地备份列表".to_owned()),
    };
    let mut choices = Vec::new();
    for entry in entries.flatten() {
        if !entry.file_type().map(|kind| kind.is_dir()).unwrap_or(false) {
            continue;
        }
        let name = entry.file_name().to_string_lossy().to_string();
        if name.starts_with('.') || !is_canonical_uuid(&name) {
            continue;
        }
        choices.push(read_choice(&entry.path(), &name));
    }
    choices.sort_by(|left, right| {
        right
            .created_at
            .cmp(&left.created_at)
            .then_with(|| right.id.cmp(&left.id))
    });
    choices.truncate(MAX_CHOICES);
    Ok(choices)
}

/// Schedules a pending restore for one selected backup package by running the
/// bundled Sidecar one-shot `prepare-restore` command. Failures leave current
/// data unchanged and never start the business HTTP API.
#[tauri::command]
pub async fn schedule_startup_restore(
    app: AppHandle,
    args: PrepareRestoreArgs,
) -> Result<ScheduledRestoreResult, String> {
    let backup_id = args.backup_id.trim().to_lowercase();
    if !is_canonical_uuid(&backup_id) {
        return Err("备份标识无效".to_owned());
    }
    if !args.confirm {
        return Err("必须明确确认后才能安排恢复".to_owned());
    }

    let app_data_dir = app
        .path()
        .app_data_dir()
        .map_err(|_| "无法定位应用数据目录".to_owned())?;
    let app_log_dir = app
        .path()
        .app_log_dir()
        .map_err(|_| "无法定位应用日志目录".to_owned())?;
    let database_path = app_data_dir.join("opc-workspace.db");
    let artifact_dir = app_data_dir.join("artifacts");
    let invoice_pdf_dir = app_data_dir.join("invoices");
    let backup_dir = app_data_dir.join("backups");

    if !backup_dir.join(&backup_id).is_dir() {
        return Err("找不到指定备份".to_owned());
    }

    let output = app
        .shell()
        .sidecar("opc-sidecar")
        .map_err(|error| format!("无法定位内置 Sidecar：{error}"))?
        .args([
            "prepare-restore".to_owned(),
            format!("--backup-id={backup_id}"),
        ])
        .env("OPC_HOST", "127.0.0.1")
        .env("OPC_PORT", "0")
        .env("OPC_DB_PATH", database_path.to_string_lossy().to_string())
        .env(
            "OPC_ARTIFACT_DIR",
            artifact_dir.to_string_lossy().to_string(),
        )
        .env(
            "OPC_INVOICE_DIR",
            invoice_pdf_dir.to_string_lossy().to_string(),
        )
        .env("OPC_BACKUP_DIR", backup_dir.to_string_lossy().to_string())
        .env("OPC_LOG_DIR", app_log_dir.to_string_lossy().to_string())
        .output()
        .await
        .map_err(|_| "无法执行启动前恢复准备".to_owned())?;

    if !output.status.success() {
        return Err("启动前恢复准备失败，当前数据未被替换".to_owned());
    }
    let stdout = String::from_utf8_lossy(&output.stdout);
    parse_prepare_restore_stdout(&stdout)
        .ok_or_else(|| "启动前恢复准备返回了无法识别的结果".to_owned())
}

fn parse_prepare_restore_stdout(stdout: &str) -> Option<ScheduledRestoreResult> {
    #[derive(Deserialize)]
    struct Envelope {
        data: SidecarPrepareResult,
    }
    #[derive(Deserialize)]
    #[serde(rename_all = "snake_case")]
    struct SidecarPrepareResult {
        backup_id: String,
        rollback_backup_id: String,
        requested_at: String,
        restart_required: bool,
    }
    for line in stdout.lines() {
        let trimmed = line.trim();
        if !trimmed.starts_with('{') {
            continue;
        }
        if let Ok(envelope) = serde_json::from_str::<Envelope>(trimmed) {
            let data = envelope.data;
            if is_canonical_uuid(&data.backup_id)
                && is_canonical_uuid(&data.rollback_backup_id)
                && data.restart_required
            {
                return Some(ScheduledRestoreResult {
                    backup_id: data.backup_id,
                    rollback_backup_id: data.rollback_backup_id,
                    requested_at: data.requested_at,
                    restart_required: data.restart_required,
                });
            }
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rejects_non_canonical_uuid() {
        assert!(is_canonical_uuid("018f0000-0000-7000-8000-00000000a001"));
        assert!(!is_canonical_uuid("not-a-uuid"));
        assert!(!is_canonical_uuid("018F0000-0000-7000-8000-00000000A001"));
        assert!(!is_canonical_uuid("../escape"));
    }

    #[test]
    fn reads_sanitized_manifest_choice() {
        let root = std::env::temp_dir().join(format!("opc-startup-restore-{}", Uuid::new_v4()));
        let id = "018f0000-0000-7000-8000-00000000a001";
        let package = root.join(id);
        fs::create_dir_all(&package).unwrap();
        fs::write(
            package.join("manifest.json"),
            format!(
                r#"{{"format_version":1,"id":"{id}","created_at":"2026-09-23T10:00:00Z","verified_at":"2026-09-23T10:01:00Z","note":"PRIVATE NOTE","app_version":"0.1.1","commit":"c","api_version":"v1","schema_version":79,"database_id":"018f0000-0000-7000-8000-00000000d001","artifact_store_id":"018f0000-0000-7000-8000-00000000e001","database":{{"path":"database/opc-workspace.db","size_bytes":1,"sha256":"{ha}"}},"artifact_marker":{{"path":"artifacts/.opc-artifact-store-v1","size_bytes":1,"sha256":"{hb}"}},"artifacts":[],"artifact_count":0,"artifact_bytes":0,"total_bytes":2,"kind":"manual"}}"#,
                id = id,
                ha = "a".repeat(64),
                hb = "b".repeat(64),
            ),
        )
        .unwrap();
        let choice = read_choice(&package, id);
        assert_eq!(choice.verification_status, "verified");
        assert_eq!(choice.kind, "manual");
        assert_eq!(choice.note.as_deref(), Some("PRIVATE NOTE"));
        assert!(!choice.note.as_deref().unwrap().contains("path"));
        let _ = fs::remove_dir_all(&root);
    }

    #[test]
    fn parses_prepare_restore_stdout() {
        let parsed = parse_prepare_restore_stdout(
            "{\"data\":{\"backup_id\":\"018f0000-0000-7000-8000-00000000a001\",\"rollback_backup_id\":\"018f0000-0000-7000-8000-00000000b001\",\"requested_at\":\"2026-09-23T10:00:00Z\",\"restart_required\":true}}\n",
        );
        let parsed = parsed.expect("parse result");
        assert_eq!(parsed.backup_id, "018f0000-0000-7000-8000-00000000a001");
        assert!(parsed.restart_required);
        assert!(parse_prepare_restore_stdout("no json").is_none());
    }
}
