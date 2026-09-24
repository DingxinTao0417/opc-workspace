//! Read-only, bounded security descriptor capture. This is not a complete SACL check.
use ::windows::Win32::{
    Foundation::HANDLE,
    Security::{
        DACL_SECURITY_INFORMATION, GROUP_SECURITY_INFORMATION, GetKernelObjectSecurity,
        LABEL_SECURITY_INFORMATION, OWNER_SECURITY_INFORMATION, PSECURITY_DESCRIPTOR,
    },
};
use std::{fs::File, os::windows::io::AsRawHandle};
// Captures owner/group/DACL/integrity label, not privileged audit SACL.
// This read-only result cannot by itself authorize replacement.
pub(super) struct SecurityDescriptor {
    pub(super) aligned: Vec<u64>,
    pub(super) length: usize,
}
impl SecurityDescriptor {
    pub(super) fn capture(file: &File) -> Result<Self, String> {
        let flags = OWNER_SECURITY_INFORMATION
            | GROUP_SECURITY_INFORMATION
            | DACL_SECURITY_INFORMATION
            | LABEL_SECURITY_INFORMATION;
        let mut length = 0;
        // SAFETY: live handle; a null output buffer only measures required size.
        let _ = unsafe {
            GetKernelObjectSecurity(HANDLE(file.as_raw_handle()), flags.0, None, 0, &mut length)
        };
        if length == 0 || length > 64 * 1024 {
            return Err("文件权限描述不可用或超限".into());
        }
        let mut result = Self {
            aligned: vec![0; (length as usize).div_ceil(8)],
            length: length as usize,
        };
        // SAFETY: aligned initialized buffer owns at least length writable bytes.
        unsafe {
            GetKernelObjectSecurity(
                HANDLE(file.as_raw_handle()),
                flags.0,
                Some(PSECURITY_DESCRIPTOR(result.aligned.as_mut_ptr().cast())),
                length,
                &mut length,
            )
        }
        .map_err(|_| "无法读取文件权限描述")?;
        if length as usize > result.length {
            return Err("文件权限在读取时变化".into());
        }
        result.length = length as usize;
        Ok(result)
    }
    pub(super) fn bytes(&self) -> &[u8] {
        // SAFETY: length was checked against the initialized owned allocation.
        unsafe { std::slice::from_raw_parts(self.aligned.as_ptr().cast(), self.length) }
    }
}
