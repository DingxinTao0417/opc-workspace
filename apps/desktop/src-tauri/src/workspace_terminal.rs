//! Explicit human PTY sessions, isolated from Agent Runtime and Sidecar tokens.
use crate::developer_workspace::WorkspaceState;
use base64::{Engine, engine::general_purpose::STANDARD};
use portable_pty::{Child, CommandBuilder, MasterPty, PtySize, native_pty_system};
use serde::Serialize;
use std::{
    collections::{HashMap, VecDeque},
    io::{Read, Write},
    sync::{Arc, Mutex},
};
use tauri::State;

const OUTPUT_LIMIT: usize = 1024 * 1024;
#[derive(Default)]
struct Output {
    bytes: VecDeque<u8>,
    start: u64,
    ended: bool,
}
impl Output {
    fn append(&mut self, data: &[u8]) {
        self.bytes.extend(data);
        let excess = self.bytes.len().saturating_sub(OUTPUT_LIMIT);
        self.bytes.drain(..excess);
        self.start += excess as u64;
    }
}

struct Session {
    // Drop the job before the ConPTY handles so descendants cannot keep them alive.
    #[cfg(windows)]
    _job: ProcessJob,
    master: Box<dyn MasterPty + Send>,
    writer: Arc<Mutex<Box<dyn Write + Send>>>,
    child: Box<dyn Child + Send + Sync>,
    output: Arc<Mutex<Output>>,
}
impl Drop for Session {
    fn drop(&mut self) {
        let _ = self.child.kill();
    }
}
#[derive(Default)]
pub struct TerminalState(Mutex<HashMap<String, Session>>);
impl TerminalState {
    pub fn shutdown(&self) {
        if let Ok(mut sessions) = self.0.lock() {
            sessions.clear();
        }
    }
}

// Owned job handles are thread-safe; closing the last handle kills the exact
// shell subtree, including on application crash. No process-name matching.
#[cfg(windows)]
struct ProcessJob(windows::Win32::Foundation::HANDLE);
#[cfg(windows)]
unsafe impl Send for ProcessJob {}
#[cfg(windows)]
impl ProcessJob {
    fn attach(pid: u32) -> Result<Self, String> {
        use windows::Win32::{
            Foundation::CloseHandle,
            System::{JobObjects::*, Threading::*},
        };
        unsafe {
            let handle = CreateJobObjectW(None, None).map_err(|_| "无法创建终端进程组")?;
            let job = Self(handle);
            let mut limits = JOBOBJECT_EXTENDED_LIMIT_INFORMATION::default();
            limits.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
            SetInformationJobObject(
                handle,
                JobObjectExtendedLimitInformation,
                &limits as *const _ as *const _,
                std::mem::size_of_val(&limits) as u32,
            )
            .map_err(|_| "无法配置终端进程组")?;
            let process = OpenProcess(PROCESS_SET_QUOTA | PROCESS_TERMINATE, false, pid)
                .map_err(|_| "无法访问终端进程")?;
            let result = AssignProcessToJobObject(handle, process);
            let _ = CloseHandle(process);
            result.map_err(|_| "无法托管终端进程树，已拒绝启动")?;
            Ok(job)
        }
    }
}
#[cfg(windows)]
impl Drop for ProcessJob {
    fn drop(&mut self) {
        unsafe {
            let _ = windows::Win32::Foundation::CloseHandle(self.0);
        }
    }
}

#[tauri::command]
pub fn workspace_terminal_start(
    roots: State<'_, WorkspaceState>,
    state: State<'_, TerminalState>,
    root_id: String,
) -> Result<String, String> {
    let root = roots.root(&root_id)?;
    start_in_root(root, &state)
}

fn start_in_root(root: std::path::PathBuf, state: &TerminalState) -> Result<String, String> {
    let mut sessions = state.0.lock().map_err(|_| "终端状态不可用")?;
    if sessions.len() >= 4 {
        return Err("最多打开 4 个终端，请先关闭一个终端标签".into());
    }
    #[cfg(not(windows))]
    return Err("手动终端目前仅在 Windows 上启用，其他平台进程树验收待完成".into());
    #[cfg(windows)]
    {
        let pair = native_pty_system()
            .openpty(PtySize {
                rows: 24,
                cols: 80,
                pixel_width: 0,
                pixel_height: 0,
            })
            .map_err(|_| "无法创建伪终端")?;
        let system_root = std::env::var_os("SystemRoot").ok_or("找不到 Windows 系统目录")?;
        let shell = std::path::PathBuf::from(system_root)
            .join("System32/WindowsPowerShell/v1.0/powershell.exe");
        let mut command = CommandBuilder::new(shell);
        command.args(["-NoLogo", "-NoProfile", "-NoExit"]);
        command.cwd(root);
        // Start with a small OS environment, not the desktop application's
        // environment (which may contain model keys and development tokens).
        command.env_clear();
        for key in [
            "PATH",
            "SystemRoot",
            "WINDIR",
            "TEMP",
            "TMP",
            "USERPROFILE",
            "APPDATA",
            "LOCALAPPDATA",
            "PATHEXT",
            "COMSPEC",
        ] {
            if let Some(value) = std::env::var_os(key) {
                command.env(key, value);
            }
        }
        command.env("TERM", "xterm-256color");
        let mut child = pair
            .slave
            .spawn_command(command)
            .map_err(|_| "PowerShell 启动失败")?;
        let job = match child
            .process_id()
            .ok_or("无法识别终端进程".to_string())
            .and_then(ProcessJob::attach)
        {
            Ok(job) => job,
            Err(error) => {
                let _ = child.kill();
                return Err(error);
            }
        };
        drop(pair.slave);
        let mut reader = pair
            .master
            .try_clone_reader()
            .map_err(|_| "终端输出不可用")?;
        let writer = pair.master.take_writer().map_err(|_| "终端输入不可用")?;
        let output = Arc::new(Mutex::new(Output::default()));
        let thread_output = output.clone();
        std::thread::spawn(move || {
            let mut buffer = [0u8; 8192];
            loop {
                match reader.read(&mut buffer) {
                    Ok(0) | Err(_) => break,
                    Ok(count) => {
                        if let Ok(mut output) = thread_output.lock() {
                            output.append(&buffer[..count]);
                        }
                    }
                }
            }
            if let Ok(mut output) = thread_output.lock() {
                output.ended = true;
            }
        });
        let id = uuid::Uuid::new_v4().to_string();
        sessions.insert(
            id.clone(),
            Session {
                master: pair.master,
                writer: Arc::new(Mutex::new(writer)),
                child,
                output,
                _job: job,
            },
        );
        Ok(id)
    }
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct TerminalOutput {
    data: String,
    next_offset: u64,
    dropped: bool,
    ended: bool,
}
#[tauri::command]
pub fn workspace_terminal_read(
    state: State<'_, TerminalState>,
    id: String,
    offset: u64,
) -> Result<TerminalOutput, String> {
    let sessions = state.0.lock().map_err(|_| "终端状态不可用")?;
    let session = sessions.get(&id).ok_or("终端已关闭")?;
    let output = session.output.lock().map_err(|_| "终端输出不可用")?;
    let from = offset
        .max(output.start)
        .min(output.start + output.bytes.len() as u64);
    let bytes: Vec<u8> = output
        .bytes
        .iter()
        .skip((from - output.start) as usize)
        .take(64 * 1024)
        .copied()
        .collect();
    Ok(TerminalOutput {
        next_offset: from + bytes.len() as u64,
        data: STANDARD.encode(bytes),
        dropped: offset < output.start,
        ended: output.ended,
    })
}
#[tauri::command]
pub async fn workspace_terminal_write(
    state: State<'_, TerminalState>,
    id: String,
    data: String,
) -> Result<(), String> {
    if data.len() > 16 * 1024 {
        return Err("单次终端输入超过 16 KiB".into());
    }
    let writer = state
        .0
        .lock()
        .map_err(|_| "终端状态不可用")?
        .get(&id)
        .ok_or("终端已关闭")?
        .writer
        .clone();
    // A busy shell may stop reading input. Never hold the session registry or
    // block Tauri's UI thread; closing the job must remain possible in this state.
    tauri::async_runtime::spawn_blocking(move || {
        let mut writer = writer.lock().map_err(|_| "终端输入不可用")?;
        writer
            .write_all(data.as_bytes())
            .and_then(|_| writer.flush())
            .map_err(|_| "终端输入失败".to_string())
    })
    .await
    .map_err(|_| "终端输入任务失败")?
}
#[tauri::command]
pub fn workspace_terminal_resize(
    state: State<'_, TerminalState>,
    id: String,
    cols: u16,
    rows: u16,
) -> Result<(), String> {
    if !(10..=400).contains(&cols) || !(2..=200).contains(&rows) {
        return Err("终端尺寸超出范围".into());
    }
    let sessions = state.0.lock().map_err(|_| "终端状态不可用")?;
    sessions
        .get(&id)
        .ok_or("终端已关闭")?
        .master
        .resize(PtySize {
            cols,
            rows,
            pixel_width: 0,
            pixel_height: 0,
        })
        .map_err(|_| "终端尺寸更新失败".into())
}
#[tauri::command]
pub fn workspace_terminal_close(state: State<'_, TerminalState>, id: String) -> Result<(), String> {
    state.0.lock().map_err(|_| "终端状态不可用")?.remove(&id);
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn output_ring_is_bounded_and_offset_preserved() {
        let mut output = Output::default();
        output.append(&vec![b'a'; OUTPUT_LIMIT]);
        output.append(b"tail");
        assert_eq!(output.bytes.len(), OUTPUT_LIMIT);
        assert_eq!(output.start, 4);
        assert_eq!(output.bytes.back(), Some(&b'l'));
    }

    #[cfg(windows)]
    #[test]
    #[ignore = "native Windows ConPTY/PowerShell probe; run explicitly with --ignored"]
    fn real_powershell_pty_has_output_resize_and_job_cleanup() {
        use windows::Win32::{
            Foundation::{CloseHandle, WAIT_OBJECT_0},
            System::Threading::{OpenProcess, PROCESS_SYNCHRONIZE, WaitForSingleObject},
        };
        let state = TerminalState::default();
        let id = start_in_root(std::env::temp_dir(), &state).unwrap();
        let process;
        {
            let mut sessions = state.0.lock().unwrap();
            let session = sessions.get_mut(&id).unwrap();
            process = unsafe {
                OpenProcess(
                    PROCESS_SYNCHRONIZE,
                    false,
                    session.child.process_id().unwrap(),
                )
                .unwrap()
            };
            session
                .master
                .resize(PtySize {
                    rows: 28,
                    cols: 92,
                    pixel_width: 0,
                    pixel_height: 0,
                })
                .unwrap();
            session
                .writer
                .lock()
                .unwrap()
                .write_all(b"Write-Output ('OPC' + '_PTY_READY')\r")
                .unwrap();
            session.writer.lock().unwrap().flush().unwrap();
        }
        let started = std::time::Instant::now();
        let mut found = false;
        let mut seen = String::new();
        while started.elapsed() < std::time::Duration::from_secs(10) {
            let mut sessions = state.0.lock().unwrap();
            let session = sessions.get_mut(&id).unwrap();
            let output = session.output.lock().unwrap();
            let bytes: Vec<u8> = output.bytes.iter().copied().collect();
            seen = String::from_utf8_lossy(&bytes).into_owned();
            if seen == "\u{1b}[6n" {
                // ConPTY asks its terminal emulator for the initial cursor.
                // xterm replies through onData; this headless probe does too.
                session
                    .writer
                    .lock()
                    .unwrap()
                    .write_all(b"\x1b[1;1R")
                    .unwrap();
                session.writer.lock().unwrap().flush().unwrap();
            }
            if String::from_utf8_lossy(&bytes).contains("OPC_PTY_READY") {
                found = true;
                break;
            }
            drop(output);
            drop(sessions);
            std::thread::sleep(std::time::Duration::from_millis(50));
        }
        state.shutdown();
        let exited = unsafe { WaitForSingleObject(process, 3000) };
        unsafe { CloseHandle(process).unwrap() };
        assert_eq!(exited, WAIT_OBJECT_0, "shell survived terminal shutdown");
        assert!(state.0.lock().unwrap().is_empty());
        assert!(
            found,
            "real PTY did not produce the expected output: {seen:?}"
        );
    }
}
