import { createServer } from "node:http";
import { readFile, writeFile } from "node:fs/promises";
import { resolve, extname } from "node:path";
import { chromium } from "playwright";

const root = resolve("out/dashboard-build");
const nodes = [
  ["web", "frontend", 3000],
  ["catalog-api", "backend", 3001],
  ["order-api", "backend", 3002],
  ["postgres", "infra", 5432],
  ["redis", "infra", 6379],
].map(([name, kind, port]) => ({
  name,
  kind,
  state: "healthy",
  ports: { http: port },
  logsAvailable: true,
  ...(kind !== "infra" ? { url: `http://localhost:${port}` } : {}),
}));
const edges = [
  [0, 1],
  [0, 2],
  [1, 3],
  [2, 3],
  [2, 4],
].map(([a, b]) => ({
  from: nodes[a].name,
  to: nodes[b].name,
  kind: "sync",
  detached: false,
  detachable: true,
  env_vars: [],
}));
const context = {
  kind: "project",
  identity: "film-demo",
  display_name: "orbit-demo",
  config_path: "/demo/orbit.yaml",
  available: true,
  running: true,
};
const resources = nodes.map((n) => ({
  ...n,
  role: n.kind,
  kind: n.kind === "infra" ? "container" : "service",
  restart_count: 0,
  external_restart_count: 0,
  logs_available: true,
}));
const status = {
  epoch: 1,
  resources,
  context,
  config_path: "/demo/orbit.yaml",
};
const api = {
  "/api/graph": { env: "orbit-demo", nodes, edges },
  "/api/status": status,
  "/api/envs": { current: "orbit-demo", running: 5, sources: [], context },
  "/api/settings": { env_toggles: {}, user_env: {}, show_history: false },
  "/api/env-toggles": [],
  "/api/version": {
    running: "1.0.0",
    on_disk: "1.0.0",
    update_available: false,
  },
  "/api/tracing/status": {
    configured: false,
    receiverHealthy: true,
    traceCount: 0,
  },
  "/api/devdb/meta": { db_configured: false, tunnel_configured: false },
  "/api/history/list": [],
  "/api/traces": [],
};
const seen = new Set();
const server = createServer(async (req, res) => {
  const path = new URL(req.url, "http://localhost").pathname;
  if (path === "/api/events") {
    res.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
    });
    res.write(`event: status\ndata: ${JSON.stringify(status)}\n\n`);
    const timer = setInterval(() => res.write(": keepalive\n\n"), 1000);
    req.on("close", () => clearInterval(timer));
    return;
  }
  if (path.startsWith("/api/")) {
    if (!(path in api)) {
      seen.add(path);
      res.writeHead(404);
      res.end(JSON.stringify({ error: "Unknown capture fixture endpoint" }));
      return;
    }
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify(api[path]));
    return;
  }
  const file = resolve(root, "." + (path === "/" ? "/index.html" : path));
  if (!file.startsWith(root + "/")) {
    res.writeHead(403);
    res.end();
    return;
  }
  try {
    const bytes = await readFile(file);
    res.setHeader(
      "Content-Type",
      {
        ".js": "text/javascript",
        ".css": "text/css",
        ".html": "text/html",
        ".svg": "image/svg+xml",
        ".png": "image/png",
      }[extname(file)] ?? "application/octet-stream",
    );
    res.end(bytes);
  } catch {
    res.writeHead(404);
    res.end();
  }
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const browser = await chromium.launch({ channel: "chromium", headless: true });
try {
  const page = await browser.newPage({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 2,
    reducedMotion: "reduce",
  });
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto(`http://127.0.0.1:${server.address().port}`, {
    waitUntil: "domcontentloaded",
  });
  await page.locator(".svelte-flow__node").first().waitFor();
  await page.waitForTimeout(1800);
  await page.screenshot({ path: "public/dashboard.png" });
  const positions = await page
    .locator(".svelte-flow__node")
    .evaluateAll((els) =>
      els.map((e) => {
        const r = e.getBoundingClientRect();
        return {
          id: e.getAttribute("data-id"),
          x: r.x,
          y: r.y,
          width: r.width,
          height: r.height,
          text: e.textContent,
        };
      }),
    );
  await writeFile(
    "public/dashboard-layout.json",
    JSON.stringify({ width: 1440, height: 900, nodes: positions }, null, 2),
  );
  console.log(
    JSON.stringify({ errors, unknownEndpoints: [...seen], positions }, null, 2),
  );
  if (errors.length || seen.size) process.exitCode = 1;
} finally {
  await browser.close();
  server.closeAllConnections();
  server.close();
}
