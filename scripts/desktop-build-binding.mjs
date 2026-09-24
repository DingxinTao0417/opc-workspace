import {
  generateKeyPairSync,
  createPublicKey,
  sign,
  verify,
  randomUUID,
} from "node:crypto";
import {
  openSync,
  closeSync,
  fsyncSync,
  writeFileSync,
  renameSync,
  unlinkSync,
  readFileSync,
} from "node:fs";
import { dirname, join } from "node:path";

export const bindingPublicKeyEnv = "OPC_DESKTOP_BINDING_PUBLIC_KEY";
export const bindingFileName = "opc-desktop-build-binding.json";
const domain = Buffer.from("OPC_DESKTOP_BUILD_BINDING_V1\0", "ascii");
function digest(value) {
  if (typeof value !== "string" || !/^[0-9a-f]{64}$/.test(value))
    throw new Error("构建绑定摘要无效");
  return Buffer.from(value, "hex");
}
export function bindingPayload(helperSha256, desktopSha256) {
  return Buffer.concat([domain, digest(helperSha256), digest(desktopSha256)]);
}
export function createBuildSigner() {
  // A fresh private KeyObject lives ONLY in this build process. Never export it,
  // write it, pass it to cargo/children, or create a reusable signing credential.
  const { privateKey, publicKey } = generateKeyPairSync("ec", {
    namedCurve: "prime256v1",
  });
  const jwk = publicKey.export({ format: "jwk" });
  const publicHex = Buffer.concat([
    Buffer.from(jwk.x, "base64url"),
    Buffer.from(jwk.y, "base64url"),
  ]).toString("hex");
  let used = false;
  return Object.freeze({
    publicHex,
    signPair(helperSha256, desktopSha256) {
      if (used) throw new Error("本次构建签名已消费");
      used = true;
      const signature = sign(
        "sha256",
        bindingPayload(helperSha256, desktopSha256),
        { key: privateKey, dsaEncoding: "ieee-p1363" },
      ).toString("hex");
      return { version: 1, helperSha256, desktopSha256, signature };
    },
  });
}
export function verifyBuildBinding(manifest, publicHex) {
  if (
    !manifest ||
    typeof manifest !== "object" ||
    Array.isArray(manifest) ||
    Object.keys(manifest).sort().join(",") !==
      "desktopSha256,helperSha256,signature,version" ||
    manifest.version !== 1 ||
    !/^[0-9a-f]{128}$/.test(manifest.signature) ||
    !/^[0-9a-f]{128}$/.test(publicHex)
  )
    return false;
  try {
    const point = Buffer.from(publicHex, "hex");
    const key = createPublicKey({
      key: {
        kty: "EC",
        crv: "P-256",
        x: point.subarray(0, 32).toString("base64url"),
        y: point.subarray(32).toString("base64url"),
      },
      format: "jwk",
    });
    return verify(
      "sha256",
      bindingPayload(manifest.helperSha256, manifest.desktopSha256),
      { key, dsaEncoding: "ieee-p1363" },
      Buffer.from(manifest.signature, "hex"),
    );
  } catch {
    return false;
  }
}
export function stageBuildBinding(destination, manifest, publicHex) {
  if (!verifyBuildBinding(manifest, publicHex))
    throw new Error("构建绑定签名核验失败");
  const bytes = Buffer.from(JSON.stringify(manifest) + "\n");
  const temporary = join(
    dirname(destination),
    `.opc-binding-stage-${randomUUID()}.tmp`,
  );
  const descriptor = openSync(temporary, "wx");
  let owned = true;
  try {
    try {
      writeFileSync(descriptor, bytes);
      fsyncSync(descriptor);
    } finally {
      closeSync(descriptor);
    }
    renameSync(temporary, destination); // Replace entry, never write through an alias.
    owned = false;
  } finally {
    if (owned) unlinkSync(temporary);
  }
  if (!readFileSync(destination).equals(bytes))
    throw new Error("构建绑定暂存期间变化");
}
