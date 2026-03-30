#!/usr/bin/env node
"use strict";

const { spawn } = require("child_process");
const { ensureRuntimeBinaries, installBinDir, releaseRepo } = require("./lib/bootstrap");

async function main() {
  try {
    const { tukuPath } = await ensureRuntimeBinaries({ silent: false });
    const env = { ...process.env };
    env.PATH = `${installBinDir()}${process.platform === "win32" ? ";" : ":"}${env.PATH || ""}`;

    const child = spawn(tukuPath, process.argv.slice(2), {
      stdio: "inherit",
      env
    });
    child.on("exit", (code, signal) => {
      if (signal) {
        process.kill(process.pid, signal);
        return;
      }
      process.exit(code || 0);
    });
  } catch (err) {
    process.stderr.write(`[tuku] failed to start: ${err.message}\n`);
    process.stderr.write(`[tuku] release repo: ${releaseRepo()}\n`);
    process.stderr.write("[tuku] check that release assets exist and are publicly downloadable.\n");
    process.exit(1);
  }
}

main();
