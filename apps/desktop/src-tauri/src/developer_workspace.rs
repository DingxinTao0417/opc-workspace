//! Human-operated desktop tools. Never exposed to remote webviews or the AI runtime.
use base64::{Engine, engine::general_purpose::STANDARD};
use serde::Serialize;
use std::{
    collections::HashMap,
    fs,
    io::Read,
    path::{Component, Path, PathBuf},
    process::{Command, Stdio},
    sync::Mutex,
    time::{Duration, Instant},
};
use tauri::State;

#[derive(Default)]
pub struct WorkspaceState(pub Mutex<HashMap<String, PathBuf>>);

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WorkspaceRoot {
    pub id: String,
    pub name: String,
    pub path: String,
}

impl WorkspaceState {
    pub fn root(&self, id: &str) -> Result<PathBuf, String> {
        self.0
            .lock()
            .map_err(|_| "目录状态不可用")?
            .get(id)
            .cloned()
            .ok_or("请重新选择工作目录".into())
    }
}

#[tauri::command]
pub async fn workspace_choose_root(
    state: State<'_, WorkspaceState>,
) -> Result<Option<WorkspaceRoot>, String> {
    let Some(folder) = rfd::AsyncFileDialog::new()
        .set_title("选择工作目录（文件与 Git 只读；AI 文件使用另行授权；终端单独启动）")
        .pick_folder()
        .await
    else {
        return Ok(None);
    };
    let path = fs::canonicalize(folder.path()).map_err(|_| "无法读取所选目录")?;
    let mut roots = state.0.lock().map_err(|_| "目录状态不可用")?;
    if let Some((id, _)) = roots.iter().find(|(_, existing)| **existing == path) {
        return Ok(Some(root_info(id.clone(), &path)));
    }
    if roots.len() >= 32 {
        return Err("本次启动最多选择 32 个目录，请重启应用后重试".into());
    }
    let id = uuid::Uuid::new_v4().to_string();
    roots.insert(id.clone(), path.clone());
    Ok(Some(root_info(id, &path)))
}

fn root_info(id: String, path: &Path) -> WorkspaceRoot {
    WorkspaceRoot {
        id,
        name: path
            .file_name()
            .unwrap_or_default()
            .to_string_lossy()
            .into(),
        path: path.display().to_string(),
    }
}

// Reject traversal, alternate streams and links/reparse points. The selected
// directory is an explicit read grant, not a sandbox against a hostile local OS.
pub fn resolve(root: &Path, relative: &str) -> Result<PathBuf, String> {
    if relative.contains([':', '\0']) || relative.len() > 4096 {
        return Err("无效的相对路径".into());
    }
    let mut target = root.to_path_buf();
    for component in Path::new(relative).components() {
        match component {
            Component::Normal(name) => {
                target.push(name);
                let meta = fs::symlink_metadata(&target).map_err(|_| "文件不存在或无法读取")?;
                if meta.file_type().is_symlink() || is_reparse(&meta) {
                    return Err("不读取符号链接或目录联接".into());
                }
            }
            _ => return Err("路径必须位于所选目录内".into()),
        }
    }
    let canonical = fs::canonicalize(&target).map_err(|_| "文件不存在或无法读取")?;
    if !canonical.starts_with(root) {
        return Err("路径超出所选目录".into());
    }
    Ok(canonical)
}

#[cfg(windows)]
fn is_reparse(meta: &fs::Metadata) -> bool {
    use std::os::windows::fs::MetadataExt;
    meta.file_attributes() & 0x400 != 0
}
#[cfg(not(windows))]
fn is_reparse(_: &fs::Metadata) -> bool {
    false
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FileEntry {
    name: String,
    path: String,
    directory: bool,
    size: u64,
}
#[derive(Serialize)]
pub struct DirectoryListing {
    entries: Vec<FileEntry>,
    truncated: bool,
}

#[tauri::command]
pub async fn workspace_list(
    state: State<'_, WorkspaceState>,
    root_id: String,
    path: String,
) -> Result<DirectoryListing, String> {
    let root = state.root(&root_id)?;
    tauri::async_runtime::spawn_blocking(move || {
        let directory = resolve(&root, &path)?;
        let items = fs::read_dir(directory).map_err(|_| "无法读取目录")?;
        let mut entries = Vec::new();
        let mut truncated = false;
        for (index, item) in items.take(1001).enumerate() {
            if index == 1000 {
                truncated = true;
                break;
            }
            let item = item.map_err(|_| "无法读取目录项")?;
            let meta = fs::symlink_metadata(item.path()).map_err(|_| "无法读取文件信息")?;
            if meta.file_type().is_symlink() || is_reparse(&meta) {
                continue;
            }
            let name = item.file_name().to_string_lossy().to_string();
            if name == ".git" {
                continue;
            }
            entries.push(FileEntry {
                path: if path.is_empty() {
                    name.clone()
                } else {
                    format!("{path}/{name}")
                },
                name,
                directory: meta.is_dir(),
                size: meta.len(),
            });
        }
        entries.sort_by(|a, b| {
            b.directory
                .cmp(&a.directory)
                .then(a.name.to_lowercase().cmp(&b.name.to_lowercase()))
        });
        Ok(DirectoryListing { entries, truncated })
    })
    .await
    .map_err(|_| "目录读取任务失败")?
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FilePreview {
    pub kind: String,
    pub content: String,
    pub size: u64,
    pub truncated: bool,
}

pub fn preview(root: &Path, path: &str) -> Result<FilePreview, String> {
    let target = resolve(root, path)?;
    let mut file = fs::File::open(&target).map_err(|_| "文件无法打开")?;
    let meta = file.metadata().map_err(|_| "文件信息不可用")?;
    if !meta.is_file() {
        return Err("只能预览普通文件".into());
    }
    let extension = target
        .extension()
        .unwrap_or_default()
        .to_string_lossy()
        .to_lowercase();
    if matches!(
        extension.as_str(),
        "pdf" | "doc" | "docx" | "xls" | "xlsx" | "ppt" | "pptx"
    ) {
        return Err("PDF / Office 文档暂不支持内联预览".into());
    }
    let mime = match extension.as_str() {
        "png" => Some("image/png"),
        "jpg" | "jpeg" => Some("image/jpeg"),
        "gif" => Some("image/gif"),
        "webp" => Some("image/webp"),
        "bmp" => Some("image/bmp"),
        _ => None,
    };
    let limit = if mime.is_some() {
        10 * 1024 * 1024
    } else {
        2 * 1024 * 1024
    };
    let mut bytes = Vec::new();
    (&mut file)
        .take(limit + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| "文件读取失败")?;
    let truncated = bytes.len() as u64 > limit;
    bytes.truncate(limit as usize);
    if let Some(mime) = mime {
        if truncated {
            return Err("图片超过 10 MiB 预览上限".into());
        }
        return Ok(FilePreview {
            kind: "image".into(),
            content: format!("data:{mime};base64,{}", STANDARD.encode(bytes)),
            size: meta.len(),
            truncated: false,
        });
    }
    if bytes.contains(&0) {
        return Err("该二进制文件暂不支持内联预览".into());
    }
    // HTML, SVG and Markdown are deliberately rendered as inert source text.
    let content = String::from_utf8_lossy(&bytes).into_owned();
    Ok(FilePreview {
        kind: "text".into(),
        content,
        size: meta.len(),
        truncated,
    })
}

#[tauri::command]
pub async fn workspace_preview(
    state: State<'_, WorkspaceState>,
    root_id: String,
    path: String,
) -> Result<FilePreview, String> {
    let root = state.root(&root_id)?;
    tauri::async_runtime::spawn_blocking(move || preview(&root, &path))
        .await
        .map_err(|_| "文件读取任务失败")?
}

fn git(root: &Path, args: &[&str]) -> Result<Vec<u8>, String> {
    let mut command = Command::new("git");
    // No inherited GIT_* overrides, credential prompts, external diff/textconv,
    // optional index writes or filesystem-monitor commands from repository config.
    command.env_clear();
    for key in [
        "PATH",
        "SystemRoot",
        "WINDIR",
        "TEMP",
        "TMP",
        "HOME",
        "USERPROFILE",
    ] {
        if let Some(value) = std::env::var_os(key) {
            command.env(key, value);
        }
    }
    command
        .env("GIT_OPTIONAL_LOCKS", "0")
        .env("GIT_TERMINAL_PROMPT", "0")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .args([
            "--no-pager",
            "-c",
            "core.fsmonitor=false",
            "-c",
            "core.quotePath=false",
            "-c",
            "color.ui=false",
        ])
        .args(args)
        .current_dir(root)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let mut child = command
        .spawn()
        .map_err(|_| "Git 不可用，请安装 Git 后重试")?;
    let stdout = child.stdout.take().ok_or("无法读取 Git 输出")?;
    let (sender, receiver) = std::sync::mpsc::channel();
    std::thread::spawn(move || {
        let mut data = Vec::new();
        let result = stdout
            .take(3 * 1024 * 1024 + 1)
            .read_to_end(&mut data)
            .map(|_| data);
        let _ = sender.send(result);
    });
    let started = Instant::now();
    let mut output = None;
    loop {
        if let Ok(result) = receiver.try_recv() {
            let data = result.map_err(|_| "Git 输出读取失败")?;
            if data.len() > 3 * 1024 * 1024 {
                let _ = child.kill();
                let _ = child.wait();
                return Err("Git 输出超过 3 MiB，请缩小变更范围".into());
            }
            output = Some(data);
        }
        if let Some(status) = child.try_wait().map_err(|_| "Git 状态不可用")? {
            if !status.success() {
                return Err("Git 读取失败；请确认目录是有效仓库".into());
            }
            let data = match output {
                Some(data) => data,
                None => receiver
                    .recv_timeout(Duration::from_secs(1))
                    .map_err(|_| "Git 输出读取超时")?
                    .map_err(|_| "Git 输出读取失败")?,
            };
            if data.len() > 3 * 1024 * 1024 {
                return Err("Git 输出超过 3 MiB，请缩小变更范围".into());
            }
            return Ok(data);
        }
        if started.elapsed() > Duration::from_secs(8) {
            let _ = child.kill();
            let _ = child.wait();
            return Err("Git 读取超时".into());
        }
        std::thread::sleep(Duration::from_millis(15));
    }
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GitReview {
    status: String,
    diff: String,
    branch: String,
}

#[tauri::command]
pub async fn workspace_git_review(
    state: State<'_, WorkspaceState>,
    root_id: String,
    staged: bool,
) -> Result<GitReview, String> {
    let root = state.root(&root_id)?;
    tauri::async_runtime::spawn_blocking(move || {
        // Require the explicitly selected repository root, not an ancestor repo.
        if !resolve(&root, ".git")?.is_dir() {
            return Err("当前审查只支持带独立 .git 目录的仓库".into());
        }
        let top = git(&root, &["rev-parse", "--show-toplevel"])?;
        let top = fs::canonicalize(String::from_utf8_lossy(&top).trim())
            .map_err(|_| "仓库根目录不可用")?;
        if top != root {
            return Err("请直接选择 Git 仓库根目录".into());
        }
        let git_dir = git(&root, &["rev-parse", "--absolute-git-dir"])?;
        let git_dir = fs::canonicalize(String::from_utf8_lossy(&git_dir).trim())
            .map_err(|_| "仓库元数据不可用")?;
        if !git_dir.starts_with(&root) {
            return Err("该工作树的 Git 元数据在所选目录之外，当前只支持独立仓库".into());
        }
        let status = git(&root, &["status", "--short", "--untracked-files=normal"])?;
        let args = if staged {
            vec!["diff", "--cached", "--no-ext-diff", "--no-textconv", "--"]
        } else {
            vec!["diff", "--no-ext-diff", "--no-textconv", "--"]
        };
        let diff = git(&root, &args)?;
        let branch = git(&root, &["rev-parse", "--abbrev-ref", "HEAD"]).unwrap_or_default();
        Ok(GitReview {
            status: String::from_utf8_lossy(&status).into(),
            diff: String::from_utf8_lossy(&diff).into(),
            branch: String::from_utf8_lossy(&branch).trim().into(),
        })
    })
    .await
    .map_err(|_| "Git 读取任务失败")?
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn rejects_escape_and_stream_paths() {
        let root = fs::canonicalize(std::env::temp_dir()).unwrap();
        for path in [
            "../secret",
            "C:/secret",
            "/etc/passwd",
            "data:stream",
            "x\0y",
        ] {
            assert!(resolve(&root, path).is_err(), "{path}");
        }
    }
    #[test]
    fn source_preview_is_inert_and_binary_rejected() {
        let root = std::env::temp_dir().join(format!("opc-preview-{}", uuid::Uuid::new_v4()));
        fs::create_dir(&root).unwrap();
        fs::write(root.join("test.html"), "<script>bad()</script>").unwrap();
        fs::write(root.join("binary.bin"), [0, 1, 2]).unwrap();
        fs::write(root.join("document.pdf"), "%PDF-1.7\nASCII-only PDF").unwrap();
        let canonical = fs::canonicalize(&root).unwrap();
        assert_eq!(preview(&canonical, "test.html").unwrap().kind, "text");
        assert!(preview(&canonical, "binary.bin").is_err());
        assert!(
            preview(&canonical, "document.pdf")
                .err()
                .unwrap()
                .contains("暂不支持")
        );
        fs::remove_file(root.join("test.html")).unwrap();
        fs::remove_file(root.join("binary.bin")).unwrap();
        fs::remove_file(root.join("document.pdf")).unwrap();
        fs::remove_dir(root).unwrap();
    }
}
