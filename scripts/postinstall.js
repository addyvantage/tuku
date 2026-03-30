#!/usr/bin/env node
"use strict";

const { ensureRuntimeBinaries, releaseRepo } = require("./lib/bootstrap");

async function main() {
  if ((process.env.TUKU_SKIP_DOWNLOAD || "").trim() === "1") {
    process.stderr.write("[tuku] skipping binary download (TUKU_SKIP_DOWNLOAD=1)\n");
    return;
  }
  try {
    await ensureRuntimeBinaries({ silent: false });
    process.stderr.write("[tuku] runtime binaries installed.\n");
  } catch (err) {
    process.stderr.write(`[tuku] postinstall warning: ${err.message}\n`);
    process.stderr.write(`[tuku] expected release repo: ${releaseRepo()}\n`);
    process.stderr.write("[tuku] binaries will be retried on first `tuku` run.\n");
  }
}

main();
