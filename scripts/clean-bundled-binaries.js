#!/usr/bin/env node
"use strict";

const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const outRoot = path.join(root, "npm-binaries");

fs.rmSync(outRoot, { recursive: true, force: true });
