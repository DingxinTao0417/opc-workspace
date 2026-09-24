//! Build-local authenticity, not Authenticode/publisher trust or user consent.
//! The trusted helper embeds a fresh public key; only the originating build
//! signs its final pair. No runtime key override, private key or signing here.
use crate::{
    file_operation_contract::{ObjectIdentity, sha256},
    file_operation_read::{PinnedDirectory, read_regular},
};
use serde::Deserialize;
use sha2::{Digest, Sha256};
use std::{
    fs::{File, OpenOptions},
    os::windows::fs::OpenOptionsExt,
    path::{Path, PathBuf},
};
use windows::{
    Win32::{Security::Cryptography::*, Storage::FileSystem::FILE_FLAG_OPEN_REPARSE_POINT},
    core::PCWSTR,
};
const DESKTOP: &str = "opc-workspace-desktop.exe";
const HELPER: &str = "opc-file-security-helper.exe";
const MANIFEST: &str = "opc-desktop-build-binding.json";
const MAX_IMAGE: usize = 128 * 1024 * 1024;
const MAX_MANIFEST: usize = 2048;
const DOMAIN: &[u8] = b"OPC_DESKTOP_BUILD_BINDING_V1\0";
type Result<T> = std::result::Result<T, String>;

fn hex<const N: usize>(value: &str) -> Result<[u8; N]> {
    if value.len() != N * 2
        || !value
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
    {
        return Err("构建绑定编码无效".into());
    }
    let mut bytes = [0; N];
    for (i, byte) in bytes.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&value[i * 2..i * 2 + 2], 16).map_err(|_| "构建绑定编码无效")?;
    }
    Ok(bytes)
}
struct ExpectedKey([u8; 64]);
impl ExpectedKey {
    fn embedded() -> Result<Self> {
        Self::parse(option_env!("OPC_DESKTOP_BINDING_PUBLIC_KEY"))
    }
    fn parse(value: Option<&str>) -> Result<Self> {
        Ok(Self(hex(value.ok_or("本次辅助构建未绑定桌面验证公钥")?)?))
    }
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct SignedManifest {
    version: u32,
    helper_sha256: String,
    desktop_sha256: String,
    signature: String,
}
struct VerifiedManifest {
    helper: String,
    desktop: String,
}
struct Algorithm(BCRYPT_ALG_HANDLE);
impl Drop for Algorithm {
    fn drop(&mut self) {
        unsafe {
            let _ = BCryptCloseAlgorithmProvider(self.0, 0);
        }
    }
}
struct Key(BCRYPT_KEY_HANDLE);
impl Drop for Key {
    fn drop(&mut self) {
        unsafe {
            let _ = BCryptDestroyKey(self.0);
        }
    }
}

fn verify(bytes: &[u8], key: &ExpectedKey) -> Result<VerifiedManifest> {
    if bytes.is_empty() || bytes.len() > MAX_MANIFEST {
        return Err("构建配对记录为空或超限".into());
    }
    let manifest: SignedManifest =
        serde_json::from_slice(bytes).map_err(|_| "构建配对记录结构无效")?;
    if manifest.version != 1 {
        return Err("构建配对记录版本不支持".into());
    }
    let mut payload = DOMAIN.to_vec();
    payload.extend(hex::<32>(&manifest.helper_sha256)?);
    payload.extend(hex::<32>(&manifest.desktop_sha256)?);
    let signature = hex::<64>(&manifest.signature)?;
    let mut provider = BCRYPT_ALG_HANDLE::default();
    unsafe {
        BCryptOpenAlgorithmProvider(
            &mut provider,
            BCRYPT_ECDSA_P256_ALGORITHM,
            PCWSTR::null(),
            BCRYPT_OPEN_ALGORITHM_PROVIDER_FLAGS(0),
        )
    }
    .ok()
    .map_err(|_| "系统签名验证器不可用")?;
    let provider = Algorithm(provider);
    let mut blob = BCRYPT_ECDSA_PUBLIC_P256_MAGIC.to_le_bytes().to_vec();
    blob.extend(32u32.to_le_bytes());
    blob.extend(key.0); // P-256 X then Y, fixed 32-byte big-endian coordinates.
    let mut imported = BCRYPT_KEY_HANDLE::default();
    unsafe {
        BCryptImportKeyPair(
            provider.0,
            None,
            BCRYPT_ECCPUBLIC_BLOB,
            &mut imported,
            &blob,
            0,
        )
    }
    .ok()
    .map_err(|_| "构建验证公钥无效")?;
    let imported = Key(imported);
    unsafe {
        BCryptVerifySignature(
            imported.0,
            None,
            &Sha256::digest(&payload),
            &signature,
            BCRYPT_FLAGS(0),
        )
    }
    .ok()
    .map_err(|_| "构建配对签名不符")?;
    Ok(VerifiedManifest {
        helper: manifest.helper_sha256,
        desktop: manifest.desktop_sha256,
    })
}

struct PinnedFile {
    file: File,
    identity: ObjectIdentity,
    modified: u64,
    digest: String,
    limit: usize,
}
impl PinnedFile {
    fn open(path: &Path, limit: usize) -> Result<(Self, Vec<u8>)> {
        let mut file = OpenOptions::new()
            .read(true)
            .share_mode(1)
            .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
            .open(path)
            .map_err(|_| "构建配对文件缺失或被占用")?;
        let (bytes, identity, modified) = read_regular(&mut file, limit)?;
        if bytes.is_empty() {
            return Err("构建配对文件为空".into());
        }
        let digest = sha256(&bytes);
        Ok((
            Self {
                file,
                identity,
                modified,
                digest,
                limit,
            },
            bytes,
        ))
    }
    fn revalidate(&mut self) -> Result<()> {
        let (bytes, identity, modified) = read_regular(&mut self.file, self.limit)?;
        if identity != self.identity || modified != self.modified || sha256(&bytes) != self.digest {
            return Err("构建配对文件已变化".into());
        }
        Ok(())
    }
}
pub(super) struct InstalledPair {
    directory: PinnedDirectory,
    _path: PathBuf,
    manifest: PinnedFile,
    helper: PinnedFile,
    desktop: PinnedFile,
}
impl InstalledPair {
    #[cfg(test)]
    pub(super) fn fixture_open(path: &Path, public_key: &str) -> Result<Self> {
        Self::open(path, &ExpectedKey::parse(Some(public_key))?)
    }
    pub(super) fn installed() -> Result<Self> {
        let key = ExpectedKey::embedded()?; // Before any manifest/path reads.
        let executable = std::env::current_exe().map_err(|_| "辅助程序位置不可用")?;
        if executable.file_name() != Some(std::ffi::OsStr::new(HELPER)) {
            return Err("辅助程序文件名不符".into());
        }
        Self::open(executable.parent().ok_or("辅助程序目录无效")?, &key)
    }
    fn open(path: &Path, key: &ExpectedKey) -> Result<Self> {
        let directory = PinnedDirectory::open(path, false)?;
        let (manifest, bytes) = PinnedFile::open(&path.join(MANIFEST), MAX_MANIFEST)?;
        let verified = verify(&bytes, key)?;
        let (helper, _) = PinnedFile::open(&path.join(HELPER), MAX_IMAGE)?;
        let (desktop, _) = PinnedFile::open(&path.join(DESKTOP), MAX_IMAGE)?;
        if helper.digest != verified.helper || desktop.digest != verified.desktop {
            return Err("安装目录内程序与本次签名配对不一致".into());
        }
        Ok(Self {
            directory,
            _path: path.to_owned(),
            manifest,
            helper,
            desktop,
        })
    }
    pub(super) fn revalidate(&mut self) -> Result<()> {
        self.manifest.revalidate()?;
        self.helper.revalidate()?;
        self.desktop.revalidate()
    }
    /// The caller must retain both this pair and Peer throughout its IPC. This
    /// verifies the expected build/file, not parent launch lineage, consent or
    /// immunity to code injection. No helper operation dispatcher calls it yet.
    pub(super) fn verify_parent(&mut self, peer: &super::transport::native::Peer) -> Result<()> {
        self.revalidate()?;
        let actual = peer.image_path()?;
        if actual.file_name() != Some(std::ffi::OsStr::new(DESKTOP)) {
            return Err("辅助调用方不是已绑定桌面程序".into());
        }
        let directory = PinnedDirectory::open(actual.parent().ok_or("调用方目录无效")?, false)?;
        let (image, _) = PinnedFile::open(&actual, MAX_IMAGE)?;
        if directory.identity != self.directory.identity
            || image.identity != self.desktop.identity
            || image.digest != self.desktop.digest
        {
            return Err("调用方映像不是本次固定的桌面对象".into());
        }
        peer.image_path()?;
        self.revalidate()
    }
}

#[cfg(test)]
#[path = "build_binding_tests.rs"]
pub(super) mod tests;
