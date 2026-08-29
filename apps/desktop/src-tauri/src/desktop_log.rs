use std::{
    fs::{self, File, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
    sync::{Arc, Mutex},
    time::{SystemTime, UNIX_EPOCH},
};

const DESKTOP_LOG_NAME: &str = "opc-workspace.log";
const DESKTOP_LOG_MAX_BYTES: u64 = 5 * 1024 * 1024;
const DESKTOP_LOG_ARCHIVES: usize = 3;

#[derive(Clone, Copy)]
pub enum DesktopEvent {
    AppSetupStarted,
    AppSetupCompleted,
    SidecarStarting,
    SidecarRestartScheduled,
    SidecarRestartExhausted,
    SidecarReady,
    SidecarError,
    SidecarShutdownStarted,
    SidecarShutdownFinished,
    ApplicationRestartRequested,
    ApplicationExitRequested,
    TrayReady,
    TrayUnavailable,
    MainWindowHiddenToTray,
    MainWindowShownFromTray,
    TrayExitRequested,
}

impl DesktopEvent {
    fn as_str(self) -> &'static str {
        match self {
            Self::AppSetupStarted => "app_setup_started",
            Self::AppSetupCompleted => "app_setup_completed",
            Self::SidecarStarting => "sidecar_starting",
            Self::SidecarRestartScheduled => "sidecar_restart_scheduled",
            Self::SidecarRestartExhausted => "sidecar_restart_exhausted",
            Self::SidecarReady => "sidecar_ready",
            Self::SidecarError => "sidecar_error",
            Self::SidecarShutdownStarted => "sidecar_shutdown_started",
            Self::SidecarShutdownFinished => "sidecar_shutdown_finished",
            Self::ApplicationRestartRequested => "application_restart_requested",
            Self::ApplicationExitRequested => "application_exit_requested",
            Self::TrayReady => "tray_ready",
            Self::TrayUnavailable => "tray_unavailable",
            Self::MainWindowHiddenToTray => "main_window_hidden_to_tray",
            Self::MainWindowShownFromTray => "main_window_shown_from_tray",
            Self::TrayExitRequested => "tray_exit_requested",
        }
    }
}

struct DesktopLogState {
    path: PathBuf,
    file: Option<File>,
    max_bytes: u64,
    archives: usize,
}

#[derive(Clone)]
pub struct DesktopLogger {
    inner: Arc<Mutex<DesktopLogState>>,
}

impl DesktopLogger {
    pub fn open(log_dir: &Path) -> Self {
        Self::open_with_limits(log_dir, DESKTOP_LOG_MAX_BYTES, DESKTOP_LOG_ARCHIVES)
    }

    pub fn stderr_only() -> Self {
        eprintln!("desktop log directory unavailable; using stderr only");
        Self {
            inner: Arc::new(Mutex::new(DesktopLogState {
                path: PathBuf::from(DESKTOP_LOG_NAME),
                file: None,
                max_bytes: DESKTOP_LOG_MAX_BYTES,
                archives: DESKTOP_LOG_ARCHIVES,
            })),
        }
    }

    fn open_with_limits(log_dir: &Path, max_bytes: u64, archives: usize) -> Self {
        let path = log_dir.join(DESKTOP_LOG_NAME);
        let file = prepare_log_file(log_dir, &path).ok();
        if file.is_none() {
            eprintln!("desktop log file unavailable; using stderr only");
        }
        Self {
            inner: Arc::new(Mutex::new(DesktopLogState {
                path,
                file,
                max_bytes,
                archives,
            })),
        }
    }

    pub fn event(&self, event: DesktopEvent) {
        let timestamp_ms = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|duration| duration.as_millis())
            .unwrap_or_default();
        let line = format!(
            "{{\"timestamp_unix_ms\":{timestamp_ms},\"component\":\"desktop\",\"event\":\"{}\"}}\n",
            event.as_str()
        );
        let mut state = self
            .inner
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.file.is_none() {
            eprint!("{line}");
            return;
        }
        if should_rotate(&state, line.len() as u64) && rotate_log(&mut state).is_err() {
            state.file = None;
            eprintln!("desktop log rotation failed; using stderr only");
            eprint!("{line}");
            return;
        }
        let write_result = state
            .file
            .as_mut()
            .ok_or(())
            .and_then(|file| file.write_all(line.as_bytes()).map_err(|_| ()))
            .and_then(|_| {
                state
                    .file
                    .as_mut()
                    .ok_or(())
                    .and_then(|file| file.sync_data().map_err(|_| ()))
            });
        if write_result.is_err() {
            state.file = None;
            eprintln!("desktop log write failed; using stderr only");
            eprint!("{line}");
        }
    }
}

fn prepare_log_file(log_dir: &Path, path: &Path) -> Result<File, ()> {
    fs::create_dir_all(log_dir).map_err(|_| ())?;
    ensure_regular_or_missing(path)?;
    OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)
        .map_err(|_| ())
}

fn ensure_regular_or_missing(path: &Path) -> Result<(), ()> {
    match fs::symlink_metadata(path) {
        Ok(metadata) if metadata.file_type().is_file() && !metadata.file_type().is_symlink() => {
            Ok(())
        }
        Ok(_) => Err(()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(_) => Err(()),
    }
}

fn should_rotate(state: &DesktopLogState, incoming_bytes: u64) -> bool {
    state
        .file
        .as_ref()
        .and_then(|file| file.metadata().ok())
        .map(|metadata| metadata.len() > 0 && metadata.len() + incoming_bytes > state.max_bytes)
        .unwrap_or(false)
}

fn archive_path(path: &Path, index: usize) -> PathBuf {
    let mut archive = path.as_os_str().to_os_string();
    archive.push(format!(".{index}"));
    PathBuf::from(archive)
}

fn rotate_log(state: &mut DesktopLogState) -> Result<(), ()> {
    state.file.take();
    for index in 1..=state.archives {
        ensure_regular_or_missing(&archive_path(&state.path, index))?;
    }
    if state.archives > 0 {
        let oldest = archive_path(&state.path, state.archives);
        if oldest.exists() {
            fs::remove_file(&oldest).map_err(|_| ())?;
        }
        for index in (1..state.archives).rev() {
            let source = archive_path(&state.path, index);
            if source.exists() {
                fs::rename(source, archive_path(&state.path, index + 1)).map_err(|_| ())?;
            }
        }
        if state.path.exists() {
            fs::rename(&state.path, archive_path(&state.path, 1)).map_err(|_| ())?;
        }
    } else if state.path.exists() {
        fs::remove_file(&state.path).map_err(|_| ())?;
    }
    state.file = Some(prepare_log_file(
        state.path.parent().ok_or(())?,
        &state.path,
    )?);
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU64, Ordering};

    static NEXT_TEST_DIR: AtomicU64 = AtomicU64::new(1);

    fn test_dir() -> PathBuf {
        std::env::temp_dir().join(format!(
            "opc-desktop-log-test-{}-{}",
            std::process::id(),
            NEXT_TEST_DIR.fetch_add(1, Ordering::Relaxed)
        ))
    }

    #[test]
    fn writes_only_whitelisted_structured_events_and_rotates() {
        let dir = test_dir();
        let logger = DesktopLogger::open_with_limits(&dir, 140, 2);
        logger.event(DesktopEvent::AppSetupStarted);
        logger.event(DesktopEvent::SidecarStarting);
        logger.event(DesktopEvent::SidecarReady);
        logger.event(DesktopEvent::TrayReady);
        logger.event(DesktopEvent::ApplicationExitRequested);

        let current = fs::read_to_string(dir.join(DESKTOP_LOG_NAME)).unwrap();
        let archived = fs::read_to_string(dir.join("opc-workspace.log.1")).unwrap();
        let joined = format!("{archived}{current}");
        assert!(joined.contains("\"component\":\"desktop\""));
        assert!(joined.contains("sidecar_ready"));
        assert!(joined.contains("tray_ready"));
        assert!(!joined.contains("token"));
        assert!(!joined.contains(dir.to_string_lossy().as_ref()));

        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn non_regular_log_target_falls_back_without_writing_through_it() {
        let dir = test_dir();
        fs::create_dir_all(dir.join(DESKTOP_LOG_NAME)).unwrap();
        let logger = DesktopLogger::open(&dir);
        logger.event(DesktopEvent::SidecarError);
        assert!(dir.join(DESKTOP_LOG_NAME).is_dir());
        fs::remove_dir_all(dir).unwrap();
    }
}
