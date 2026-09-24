import { execFileSync } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import {
  constants,
  copyFileSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  renameSync,
  unlinkSync,
} from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import {
  bindingPublicKeyEnv,
  bindingFileName,
  createBuildSigner,
  stageBuildBinding,
} from "./desktop-build-binding.mjs";

const repoRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const desktop = join(repoRoot, "apps/desktop");
export const helperDigestKey = "OPC_FILE_HELPER_SHA256";

export function buildOptions(args, platform) {
  if (
    args.some((a) => !["--debug", "--no-bundle"].includes(a)) ||
    new Set(args).size !== args.length
  )
    throw new Error(
      "受绑定的本机构建仅支持 --debug / --no-bundle；不接受自定义 target、runner 或 config。",
    );
  return {
    withHelper: platform === "win32",
    profile: args.includes("--debug") ? "debug" : "release",
    args,
  };
}
export function boundEnvironment(environment, digest) {
  const result = { ...environment };
  // Never inherit a stale/custom runtime digest from the invoking environment.
  delete result[helperDigestKey];
  delete result[bindingPublicKeyEnv];
  if (digest !== null) {
    if (!/^[0-9a-f]{64}$/.test(digest)) throw new Error("辅助程序构建摘要无效");
    result[helperDigestKey] = digest;
  }
  return result;
}
function sha256(file) {
  const stat = lstatSync(file);
  if (
    !stat.isFile() ||
    stat.isSymbolicLink() ||
    stat.size === 0 ||
    stat.size > 128 * 1024 * 1024
  )
    throw new Error("辅助程序产物必须是有界普通文件");
  return createHash("sha256").update(readFileSync(file)).digest("hex");
}
export function stageHelper(source, binaryDir, triple) {
  if (!/^[a-z0-9_]+-pc-windows-(msvc|gnu)$/.test(triple))
    throw new Error("不支持的 Windows 本机构建目标");
  mkdirSync(binaryDir, { recursive: true });
  const staged = join(binaryDir, `opc-file-security-helper-${triple}.exe`);
  return stageImage(source, staged);
}
export function stageImage(source, staged) {
  const digest = sha256(source);
  // Replace the entry, never write through an existing symlink or hardlink.
  // Build directories remain part of the trusted local build environment.
  const temporary = join(
    dirname(staged),
    `.opc-helper-stage-${randomUUID()}.tmp`,
  );
  copyFileSync(source, temporary, constants.COPYFILE_EXCL);
  let ownedTemporary = true;
  try {
    if (sha256(temporary) !== digest)
      throw new Error("辅助程序复制期间发生变化");
    renameSync(temporary, staged);
    ownedTemporary = false;
  } finally {
    if (ownedTemporary) unlinkSync(temporary);
  }
  if (sha256(staged) !== digest || lstatSync(staged).nlink !== 1)
    throw new Error("暂存辅助程序与已构建产物不一致");
  return digest;
}
export function buildDesktop(args = process.argv.slice(2)) {
  const options = buildOptions(args, process.platform);
  let digest = null;
  let config = [];
  let pair = null;
  const clean = boundEnvironment(process.env, null);
  if (options.withHelper) {
    const triple = execFileSync("rustc", ["--print", "host-tuple"], {
      encoding: "utf8",
      env: clean,
    }).trim();
    if (!/^[a-z0-9_]+-pc-windows-(msvc|gnu)$/.test(triple))
      throw new Error("不支持的 Windows 本机构建目标");
    const helperDir = join(repoRoot, "apps/file-security-helper");
    const signer = createBuildSigner();
    execFileSync(
      "cargo",
      [
        "build",
        "--locked",
        "--manifest-path",
        join(helperDir, "Cargo.toml"),
        "--target-dir",
        join(helperDir, "target"),
        "--target",
        triple,
        ...(options.profile === "release" ? ["--release"] : []),
      ],
      {
        cwd: repoRoot,
        env: { ...clean, [bindingPublicKeyEnv]: signer.publicHex },
        stdio: "inherit",
      },
    );
    const source = join(
      helperDir,
      "target",
      triple,
      options.profile,
      "opc-file-security-helper.exe",
    );
    digest = stageHelper(source, join(desktop, "src-tauri/binaries"), triple);
    pair = { signer, triple, source };
    // Overlay only this build. Ordinary cargo check/dev do not require staged
    // binaries or silently acquire a helper trust policy.
    config = [
      "--target",
      triple,
      "--config",
      JSON.stringify({
        bundle: {
          externalBin: [
            "binaries/opc-sidecar",
            "binaries/opc-file-security-helper",
          ],
        },
      }),
    ];
  }
  // Calling the already-installed JS CLI avoids .cmd shell parsing on Windows.
  execFileSync(
    process.execPath,
    [
      join(repoRoot, "node_modules/@tauri-apps/cli/tauri.js"),
      "build",
      ...(options.withHelper
        ? [...options.args.filter((a) => a !== "--no-bundle"), "--no-bundle"]
        : options.args),
      ...config,
    ],
    { cwd: desktop, env: boundEnvironment(clean, digest), stdio: "inherit" },
  );
  if (pair) {
    // Break the mutual-digest cycle: public key -> helper -> desktop -> signed
    // pair manifest. No private key material ever goes to a child/environment.
    const output = join(
      desktop,
      "src-tauri/target",
      pair.triple,
      options.profile,
    );
    const mainImage = join(output, "opc-workspace-desktop.exe");
    const helperImage = join(output, "opc-file-security-helper.exe");
    if (sha256(pair.source) !== digest || sha256(helperImage) !== digest)
      throw new Error("构建期间辅助产物发生变化");
    // Cargo can hardlink the top-level executable to its deps cache. Replace
    // only the deliverable entry with an independent identical copy, leaving
    // the cache object/aliases intact. Runtime no-hardlink checks stay strict.
    const desktopDigest = stageImage(mainImage, mainImage);
    const manifest = pair.signer.signPair(digest, desktopDigest);
    const bindingPath = join(output, bindingFileName);
    stageBuildBinding(bindingPath, manifest, pair.signer.publicHex);
    // Native read-only verification uses the key embedded by this helper build,
    // not a public key supplied on argv. It never starts the desktop or UAC.
    execFileSync(helperImage, ["--verify-build"], {
      cwd: output,
      env: clean,
      stdio: "inherit",
    });
    if (!options.args.includes("--no-bundle")) {
      execFileSync(
        process.execPath,
        [
          join(repoRoot, "node_modules/@tauri-apps/cli/tauri.js"),
          "bundle",
          ...(options.profile === "debug" ? ["--debug"] : []),
          ...config,
          "--config",
          JSON.stringify({
            bundle: { resources: { [bindingPath]: bindingFileName } },
          }),
        ],
        {
          cwd: desktop,
          env: boundEnvironment(clean, digest),
          stdio: "inherit",
        },
      );
      // Signing/hooks that change either image invalidate the manifest. Do not
      // report such bundles as valid; release signing order is a separate gate.
      if (
        sha256(mainImage) !== manifest.desktopSha256 ||
        sha256(helperImage) !== digest
      )
        throw new Error(
          "打包改变了已绑定映像；产物不可交付，需先处理最终签名顺序",
        );
    }
  }
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
)
  buildDesktop();
