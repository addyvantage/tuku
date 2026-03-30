"use strict";

const fs = require("fs");
const os = require("os");
const path = require("path");
const https = require("https");
const cp = require("child_process");

function isWindows() {
  return process.platform === "win32";
}

function executableName(base) {
  return isWindows() ? `${base}.exe` : base;
}

function resolvePlatform() {
  if (process.platform === "darwin") return "darwin";
  if (process.platform === "linux") return "linux";
  if (process.platform === "win32") return "windows";
  throw new Error(`unsupported platform: ${process.platform}`);
}

function resolveArch() {
  if (process.arch === "arm64") return "arm64";
  if (process.arch === "x64") return "amd64";
  throw new Error(`unsupported architecture: ${process.arch}`);
}

function releaseRepo() {
  const fromEnv = (process.env.TUKU_RELEASE_REPO || "").trim();
  if (fromEnv) return fromEnv;

  const fromNpmConfig = (process.env.npm_package_config_releaseRepo || "").trim();
  if (fromNpmConfig) return fromNpmConfig;

  return "kagaya/Tuku";
}

function assetPrefix() {
  const fromEnv = (process.env.TUKU_ASSET_PREFIX || "").trim();
  if (fromEnv) return fromEnv;

  const fromNpmConfig = (process.env.npm_package_config_assetPrefix || "").trim();
  if (fromNpmConfig) return fromNpmConfig;

  return "tuku";
}

function releaseVersion() {
  const fromEnv = (process.env.TUKU_CLI_VERSION || "").trim();
  if (fromEnv) return fromEnv;
  const pkgVersion = (process.env.npm_package_version || "").trim();
  if (pkgVersion) return pkgVersion;
  return "latest";
}

function installRoot() {
  const fromEnv = (process.env.TUKU_INSTALL_ROOT || "").trim();
  if (fromEnv) return fromEnv;
  return path.join(os.homedir(), ".tukuai");
}

function installBinDir() {
  return path.join(installRoot(), "bin");
}

function binaryPath(baseName) {
  return path.join(installBinDir(), executableName(baseName));
}

function packageBuildStampMs() {
  const root = packageRoot();
  const candidates = [
    path.join(root, "package.json"),
    path.join(root, "scripts", "tuku.js"),
    path.join(root, "cmd", "tuku", "main.go"),
    path.join(root, "cmd", "tukud", "main.go")
  ];
  let latest = 0;
  for (const file of candidates) {
    try {
      const st = fs.statSync(file);
      latest = Math.max(latest, st.mtimeMs || 0);
    } catch (_err) {
      // ignore missing candidate files
    }
  }
  return latest;
}

function binaryNeedsRefresh(target) {
  if ((process.env.TUKU_FORCE_BOOTSTRAP || "").trim() === "1") {
    return true;
  }
  try {
    if (!fs.existsSync(target)) return true;
    const binMtime = fs.statSync(target).mtimeMs || 0;
    const stamp = packageBuildStampMs();
    return stamp > 0 && binMtime < stamp;
  } catch (_err) {
    return true;
  }
}

function releaseTag() {
  const version = releaseVersion();
  return version === "latest" ? "latest" : `v${version.replace(/^v/, "")}`;
}

function binaryAssetName(baseName) {
  const prefix = assetPrefix();
  const platform = resolvePlatform();
  const arch = resolveArch();
  return `${prefix}-${baseName}-${platform}-${arch}${isWindows() ? ".exe" : ""}`;
}

function binaryUrls(baseName) {
  const repo = releaseRepo();
  const name = binaryAssetName(baseName);
  const tag = releaseTag();

  if (tag === "latest") {
    return [
      `https://github.com/${repo}/releases/latest/download/${name}`,
      `https://github.com/${repo}/releases/download/latest/${name}`
    ];
  }

  const clean = tag.replace(/^v/, "");
  return [
    `https://github.com/${repo}/releases/download/${tag}/${name}`,
    `https://github.com/${repo}/releases/download/${clean}/${name}`
  ];
}

function mkdirp(dir) {
  fs.mkdirSync(dir, { recursive: true });
}

function downloadFile(url, dest) {
  return new Promise((resolve, reject) => {
    const req = https.get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        return resolve(downloadFile(res.headers.location, dest));
      }
      if (res.statusCode !== 200) {
        res.resume();
        return reject(new Error(`download failed (${res.statusCode}) for ${url}`));
      }

      const tmp = `${dest}.tmp-${Date.now()}`;
      const out = fs.createWriteStream(tmp, { mode: 0o755 });
      res.pipe(out);
      out.on("finish", () => {
        out.close(() => {
          fs.renameSync(tmp, dest);
          if (!isWindows()) {
            fs.chmodSync(dest, 0o755);
          }
          resolve();
        });
      });
      out.on("error", (err) => reject(err));
    });
    req.on("error", (err) => reject(err));
    req.setTimeout(30000, () => {
      req.destroy(new Error("download timeout"));
    });
  });
}

async function ensureBinary(baseName, options = {}) {
  const target = binaryPath(baseName);
  if (fs.existsSync(target) && !binaryNeedsRefresh(target)) return target;
  if (fs.existsSync(target)) {
    fs.rmSync(target, { force: true });
  }

  mkdirp(installBinDir());
  const urls = binaryUrls(baseName);
  let lastErr = null;
  for (const url of urls) {
    try {
      if (!options.silent) {
        process.stderr.write(`[tuku] downloading ${baseName} from ${url}\n`);
      }
      await downloadFile(url, target);
      return target;
    } catch (err) {
      lastErr = err;
    }
  }
  throw lastErr || new Error(`failed to download binary ${baseName}`);
}

function packageRoot() {
  return path.resolve(__dirname, "..", "..");
}

function canBuildFromPackageSource() {
  const root = packageRoot();
  return (
    fs.existsSync(path.join(root, "go.mod")) &&
    fs.existsSync(path.join(root, "cmd", "tuku", "main.go")) &&
    fs.existsSync(path.join(root, "cmd", "tukud", "main.go"))
  );
}

function hasGoToolchain() {
  try {
    cp.execFileSync("go", ["version"], { stdio: "ignore" });
    return true;
  } catch (_err) {
    return false;
  }
}

function buildBinaryFromPackageSource(baseName, dest, options = {}) {
  const root = packageRoot();
  const rel = `./cmd/${baseName}`;
  if (!options.silent) {
    process.stderr.write(`[tuku] building ${baseName} from bundled source (${rel})\n`);
  }
  cp.execFileSync("go", ["build", "-o", dest, rel], {
    cwd: root,
    stdio: options.silent ? "ignore" : "inherit",
    env: process.env
  });
  if (!isWindows()) {
    fs.chmodSync(dest, 0o755);
  }
}

function buildMissingFromSource(missing, options = {}) {
  if (missing.length === 0) return;
  if (!canBuildFromPackageSource()) {
    throw new Error("package does not include Go source fallback");
  }
  if (!hasGoToolchain()) {
    throw new Error("Go toolchain not found for source fallback build");
  }

  mkdirp(installBinDir());
  for (const name of missing) {
    const dest = binaryPath(name);
    if (fs.existsSync(dest)) continue;
    buildBinaryFromPackageSource(name, dest, options);
  }
}

async function ensureRuntimeBinaries(options = {}) {
  const names = ["tuku", "tukud"];
  const downloadErrors = {};

  for (const name of names) {
    const target = binaryPath(name);
    if (fs.existsSync(target) && binaryNeedsRefresh(target)) {
      fs.rmSync(target, { force: true });
    }
    if (fs.existsSync(target)) continue;
    try {
      await ensureBinary(name, options);
    } catch (err) {
      downloadErrors[name] = err;
    }
  }

  const stillMissing = names.filter((n) => !fs.existsSync(binaryPath(n)));
  if (stillMissing.length > 0) {
    if (!options.silent) {
      process.stderr.write("[tuku] release download unavailable, attempting source fallback build...\n");
    }
    try {
      buildMissingFromSource(stillMissing, options);
    } catch (buildErr) {
      const details = stillMissing
        .map((name) => `${name}: ${(downloadErrors[name] && downloadErrors[name].message) || "download failed"}`)
        .join("; ");
      throw new Error(`${details}; source fallback failed: ${buildErr.message}`);
    }
  }

  const tukuPath = binaryPath("tuku");
  if (!fs.existsSync(tukuPath)) {
    throw new Error("tuku binary is missing after bootstrap");
  }
  return { tukuPath };
}

module.exports = {
  ensureRuntimeBinaries,
  installBinDir,
  binaryPath,
  releaseRepo,
  binaryUrls
};
