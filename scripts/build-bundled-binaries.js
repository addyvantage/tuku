#!/usr/bin/env node
"use strict";

const cp = require("child_process");
const fs = require("fs");
const path = require("path");
const zlib = require("zlib");

const root = path.resolve(__dirname, "..");
const outRoot = path.join(root, "npm-binaries");
const pkg = JSON.parse(fs.readFileSync(path.join(root, "package.json"), "utf8"));

const targets = [
  { goos: "darwin", goarch: "arm64" },
  { goos: "darwin", goarch: "amd64" },
  { goos: "linux", goarch: "arm64" },
  { goos: "linux", goarch: "amd64" }
];

function executableName(base, goos) {
  return goos === "windows" ? `${base}.exe` : base;
}

function mkdirp(dir) {
  fs.mkdirSync(dir, { recursive: true });
}

function rmrf(target) {
  fs.rmSync(target, { recursive: true, force: true });
}

function buildBinary(baseName, target) {
  const outDir = path.join(outRoot, `${target.goos}-${target.goarch}`);
  const binaryName = executableName(baseName, target.goos);
  const binaryPath = path.join(outDir, binaryName);
  const archivePath = `${binaryPath}.gz`;

  mkdirp(outDir);
  cp.execFileSync(
    "go",
    ["build", "-trimpath", "-ldflags", "-s -w", "-o", binaryPath, `./cmd/${baseName}`],
    {
      cwd: root,
      stdio: "inherit",
      env: {
        ...process.env,
        CGO_ENABLED: "0",
        GOOS: target.goos,
        GOARCH: target.goarch
      }
    }
  );
  const raw = fs.readFileSync(binaryPath);
  const compressed = zlib.gzipSync(raw, { level: zlib.constants.Z_BEST_COMPRESSION });
  fs.writeFileSync(archivePath, compressed);
  fs.rmSync(binaryPath, { force: true });
  return archivePath;
}

function main() {
  if ((process.env.TUKU_SKIP_BUNDLED_BINARIES || "").trim() === "1") {
    process.stderr.write("[tuku] skipping bundled npm binary build (TUKU_SKIP_BUNDLED_BINARIES=1)\n");
    return;
  }

  rmrf(outRoot);
  mkdirp(outRoot);
  const built = [];
  for (const target of targets) {
    for (const baseName of ["tuku", "tukud"]) {
      process.stderr.write(`[tuku] bundling ${baseName} for ${target.goos}-${target.goarch}\n`);
      built.push(path.relative(root, buildBinary(baseName, target)));
    }
  }
  fs.writeFileSync(
    path.join(outRoot, "manifest.json"),
    JSON.stringify(
      {
        packageName: pkg.name,
        packageVersion: pkg.version,
        builtAt: new Date().toISOString(),
        targets: targets.map((t) => `${t.goos}-${t.goarch}`),
        files: built
      },
      null,
      2
    ) + "\n"
  );
}

main();
