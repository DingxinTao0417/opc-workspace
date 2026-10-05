//! A kill-on-close Windows Job Object that owns one process tree.
//!
//! Closing the last handle to the job terminates every process in it, and the
//! operating system closes that handle when this process dies for any reason.
//! Processes created by a member join the job automatically, so the whole
//! subtree is covered without matching process names. A job created by a
//! member for its own children nests inside this one.
//!
//! There is no equivalent on other platforms in this module: callers there get
//! no process-tree governance.

#[cfg(windows)]
pub(crate) use imp::{JobAttachStep, ProcessJob};

#[cfg(windows)]
mod imp {
    use windows::Win32::{
        Foundation::{CloseHandle, ERROR_INVALID_PARAMETER, HANDLE, STILL_ACTIVE},
        System::{JobObjects::*, Threading::*},
    };

    /// The step at which attaching a process to a new job failed.
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub(crate) enum JobAttachStep {
        Create,
        Configure,
        OpenProcess,
        Assign,
    }

    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub(crate) struct JobAttachError {
        pub(crate) step: JobAttachStep,
        /// The process had already ended, so there was nothing left to govern.
        pub(crate) process_exited: bool,
    }

    pub(crate) struct ProcessJob(HANDLE);

    // The handle is owned by this value and is not inheritable; the kernel
    // object itself is safe to use from any thread.
    unsafe impl Send for ProcessJob {}

    impl ProcessJob {
        /// Creates a kill-on-close job and puts the process into it.
        pub(crate) fn attach(pid: u32) -> Result<Self, JobAttachError> {
            let failed = |step, process_exited| JobAttachError {
                step,
                process_exited,
            };
            unsafe {
                let handle = CreateJobObjectW(None, None)
                    .map_err(|_| failed(JobAttachStep::Create, false))?;
                let job = Self(handle);
                let mut limits = JOBOBJECT_EXTENDED_LIMIT_INFORMATION::default();
                limits.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
                SetInformationJobObject(
                    handle,
                    JobObjectExtendedLimitInformation,
                    &limits as *const _ as *const _,
                    std::mem::size_of_val(&limits) as u32,
                )
                .map_err(|_| failed(JobAttachStep::Configure, false))?;
                let process = OpenProcess(
                    PROCESS_SET_QUOTA | PROCESS_TERMINATE | PROCESS_QUERY_LIMITED_INFORMATION,
                    false,
                    pid,
                )
                .map_err(|error| {
                    // A process ID that no longer resolves means the process
                    // is already gone.
                    failed(
                        JobAttachStep::OpenProcess,
                        error.code() == ERROR_INVALID_PARAMETER.to_hresult(),
                    )
                })?;
                let assigned = AssignProcessToJobObject(handle, process);
                let exited = assigned.is_err() && process_has_exited(process);
                let _ = CloseHandle(process);
                assigned.map_err(|_| failed(JobAttachStep::Assign, exited))?;
                Ok(job)
            }
        }

        /// Ends every process in the job with the given exit code.
        pub(crate) fn terminate(&self, exit_code: u32) -> Result<(), String> {
            unsafe { TerminateJobObject(self.0, exit_code).map_err(|error| error.to_string()) }
        }
    }

    fn process_has_exited(process: HANDLE) -> bool {
        let mut code = 0u32;
        // The caller holds an open handle with query access.
        let read = unsafe { GetExitCodeProcess(process, &mut code) };
        read.is_ok() && code != STILL_ACTIVE.0 as u32
    }

    impl Drop for ProcessJob {
        fn drop(&mut self) {
            unsafe {
                let _ = CloseHandle(self.0);
            }
        }
    }
}

#[cfg(all(test, windows))]
pub(crate) mod tests;
