#!/usr/bin/env node

import { createServer } from "node:http";

const port = Number(process.env.FAKE_RELAY_PORT);
const server = createServer((request, response) => {
  if (request.method !== "POST") {
    response.writeHead(405);
    response.end();
    return;
  }
  request.resume();
  request.on("end", () => {
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ jsonrpc: "2.0", id: 1, result: {} }));
  });
});

server.listen(port, "127.0.0.1");
const stop = () => server.close(() => process.exit(0));
process.once("SIGINT", stop);
process.once("SIGTERM", stop);
