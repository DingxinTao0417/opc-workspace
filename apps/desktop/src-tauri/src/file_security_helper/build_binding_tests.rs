use super::*;
use std::fs;
// Real CNG signing for isolated process fixtures only. Keys are ephemeral,
// never exported privately or persisted; no certificate store/privilege use.
pub(crate) fn sign_pair(helper: &[u8], desktop: &[u8]) -> (String, Vec<u8>) {
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
    .unwrap();
    let provider = Algorithm(provider);
    let mut key = BCRYPT_KEY_HANDLE::default();
    unsafe { BCryptGenerateKeyPair(provider.0, &mut key, 256, 0) }
        .ok()
        .unwrap();
    let key = Key(key);
    unsafe { BCryptFinalizeKeyPair(key.0, 0) }.ok().unwrap();
    let mut public = [0u8; 72];
    let mut size = 0;
    unsafe {
        BCryptExportKey(
            key.0,
            None,
            BCRYPT_ECCPUBLIC_BLOB,
            Some(&mut public),
            &mut size,
            0,
        )
    }
    .ok()
    .unwrap();
    assert_eq!(size, 72);
    let helper = sha256(helper);
    let desktop = sha256(desktop);
    let mut payload = DOMAIN.to_vec();
    payload.extend(hex::<32>(&helper).unwrap());
    payload.extend(hex::<32>(&desktop).unwrap());
    let mut signature = [0u8; 64];
    unsafe {
        BCryptSignHash(
            key.0,
            None,
            &Sha256::digest(payload),
            Some(&mut signature),
            &mut size,
            BCRYPT_FLAGS(0),
        )
    }
    .ok()
    .unwrap();
    assert_eq!(size, 64);
    let encode = |bytes: &[u8]| bytes.iter().map(|b| format!("{b:02x}")).collect::<String>();
    (encode(&public[8..]), serde_json::to_vec(&serde_json::json!({"version": 1, "helperSha256": helper, "desktopSha256": desktop, "signature": encode(&signature)})).unwrap())
}
fn vector() -> (ExpectedKey, Vec<u8>) {
    // Node-generated PUBLIC test vector, no private key retained in the repo.
    let value: serde_json::Value =
        serde_json::from_str(include_str!("build_binding_fixture.json")).unwrap();
    (
        ExpectedKey::parse(value["publicKey"].as_str()).unwrap(),
        serde_json::to_vec(&value["manifest"]).unwrap(),
    )
}
#[test]
fn node_signature_is_verified_by_real_windows_cng() {
    let (key, bytes) = vector();
    let manifest = verify(&bytes, &key).unwrap();
    assert_eq!(manifest.helper, sha256(b"fixture helper"));
    assert_eq!(manifest.desktop, sha256(b"fixture desktop"));
}
#[test]
fn altered_digests_signature_or_public_key_are_not_trusted() {
    let (key, bytes) = vector();
    for field in ["helperSha256", "desktopSha256", "signature"] {
        let mut value: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
        let mut text = value[field].as_str().unwrap().as_bytes().to_vec();
        text[0] = if text[0] == b'0' { b'1' } else { b'0' };
        value[field] = String::from_utf8(text).unwrap().into();
        assert!(verify(&serde_json::to_vec(&value).unwrap(), &key).is_err());
    }
    let mut wrong = key.0;
    wrong[0] ^= 1;
    assert!(verify(&bytes, &ExpectedKey(wrong)).is_err());
}
#[test]
fn missing_key_and_noncanonical_or_extended_records_fail_closed() {
    for key in [None, Some(""), Some("AB"), Some(&"ab".repeat(63))] {
        assert!(ExpectedKey::parse(key).is_err());
    }
    let (key, bytes) = vector();
    for invalid in [
        vec![],
        vec![b' '; MAX_MANIFEST + 1],
        br#"{"version":1,"version":1}"#.to_vec(),
    ] {
        assert!(verify(&invalid, &key).is_err());
    }
    for (field, value) in [
        ("version", 2.into()),
        ("approved", true.into()),
        ("helperSha256", "AB".repeat(32).into()),
    ] {
        let mut doc: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
        doc[field] = value;
        assert!(verify(&serde_json::to_vec(&doc).unwrap(), &key).is_err());
    }
}
struct Fixture(PathBuf);
impl Fixture {
    fn new() -> Self {
        let path =
            std::env::temp_dir().join(format!("opc-build-pair-test-{}", uuid::Uuid::new_v4()));
        fs::create_dir(&path).unwrap();
        fs::write(path.join(HELPER), b"fixture helper").unwrap();
        fs::write(path.join(DESKTOP), b"fixture desktop").unwrap();
        fs::write(path.join(MANIFEST), vector().1).unwrap();
        Self(path)
    }
    fn pair(&self) -> Result<InstalledPair> {
        InstalledPair::open(&self.0, &vector().0)
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        assert!(self.0.is_absolute() && self.0.parent() == Some(std::env::temp_dir().as_path()));
        assert!(
            self.0
                .file_name()
                .unwrap()
                .to_string_lossy()
                .starts_with("opc-build-pair-test-")
        );
        fs::remove_dir_all(&self.0).unwrap();
    }
}
#[test]
fn pair_pins_all_files_and_rejects_unrelated_parent_process() {
    let f = Fixture::new();
    let mut pair = f.pair().unwrap();
    for name in [MANIFEST, HELPER, DESKTOP] {
        assert!(fs::write(f.0.join(name), b"changed").is_err());
    }
    assert!(fs::rename(&f.0, f.0.with_extension("moved")).is_err());
    pair.revalidate().unwrap();
    assert!(
        pair.verify_parent(
            &super::super::transport::native::Peer::pin(std::process::id()).unwrap()
        )
        .is_err()
    );
}
#[test]
fn modified_missing_or_linked_artifacts_do_not_downgrade_to_hash_only() {
    for name in [MANIFEST, HELPER, DESKTOP] {
        for mode in ["changed", "missing", "link"] {
            let f = Fixture::new();
            let path = f.0.join(name);
            match mode {
                "changed" => fs::write(&path, b"changed").unwrap(),
                "missing" => fs::remove_file(&path).unwrap(),
                _ => fs::hard_link(&path, f.0.join("alias")).unwrap(),
            }
            assert!(f.pair().is_err());
        }
    }
}
#[test]
fn late_alias_invalidates_previously_verified_pair() {
    for name in [MANIFEST, HELPER, DESKTOP] {
        let f = Fixture::new();
        let mut pair = f.pair().unwrap();
        fs::hard_link(f.0.join(name), f.0.join("late-alias")).unwrap();
        assert!(pair.revalidate().is_err());
    }
}
