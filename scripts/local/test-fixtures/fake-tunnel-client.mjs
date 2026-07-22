#!/usr/bin/env node

import { appendFileSync } from "node:fs";

const args = process.argv.slice(2);
if (process.env.FAKE_TUNNEL_LOG) {
  appendFileSync(process.env.FAKE_TUNNEL_LOG, `${JSON.stringify(args)}\n`);
}

if (process.env.FAKE_TUNNEL_FAIL_ON && args.includes(process.env.FAKE_TUNNEL_FAIL_ON)) {
  console.error("fake tunnel-client failure");
  process.exit(7);
}

if (args.includes("status") && process.env.FAKE_TUNNEL_STATUS_OUTPUT) {
  process.stdout.write(process.env.FAKE_TUNNEL_STATUS_OUTPUT);
}

process.exit(0);
