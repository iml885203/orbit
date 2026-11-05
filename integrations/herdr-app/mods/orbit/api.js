import { exec, home } from "herdr";

const LOG_LINES = 1000;
// Herdr's exec stops a program after 30 s, so a restart or stop asks Orbit to give up a little
// sooner and report `timeout` itself instead of being cut off mid-wait.
const ACTION_TIMEOUT = "25s";
const TRACE_LIMIT = 100;
const SQL_DIFF_CONCURRENCY = 4;

export function errorText(error) {
  return error?.message ?? String(error);
}

export function firstLine(text) {
  return String(text ?? "").trim().split("\n")[0].slice(0, 300);
}

export function asArray(value) {
  return Array.isArray(value) ? value : [];
}

export function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

export function asText(value, fallback = "") {
  if (typeof value === "string") return value;
  if (typeof value === "number" && Number.isFinite(value)) return String(value);
  return fallback;
}

export function asNumber(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : 0;
}

function parseJson(text, what) {
  try {
    return JSON.parse(text);
  } catch {
    throw new Error(`${what}: the response is not JSON (${firstLine(text) || "empty"})`);
  }
}

function parseHealth(raw) {
  const health = asObject(raw);
  const attempts = asNumber(health.attempts);
  const max = asNumber(health.max_retries);
  return attempts || max ? `${attempts}/${max}` : "";
}

function parseSidecars(raw) {
  return asArray(raw).map(asObject).filter((one) => asText(one.url)).map((one) => ({ name: asText(one.name, "sidecar"), url: asText(one.url) }));
}

function byKindAndName(a, b) {
  return a.kind === b.kind ? a.name.localeCompare(b.name) : a.kind === "service" ? -1 : b.kind === "service" ? 1 : a.kind.localeCompare(b.kind);
}

export function sortResources(resources) {
  return [...resources].sort(byKindAndName);
}

export function parseResource(raw) {
  const r = asObject(raw);
  return {
    name: asText(r.name, "?"), kind: asText(r.kind), role: asText(r.role), state: asText(r.state, "unknown"),
    phase: asText(r.starting_phase), logs: r.logs_available === true, restarts: asNumber(r.restart_count),
    ports: Object.entries(asObject(r.ports)).map(([key, value]) => `${key} ${asText(value)}`),
    url: asText(r.url), uptime: asText(r.uptime), startup: asText(r.startup_time), image: asText(r.image),
    health: parseHealth(r.health_progress), sidecars: parseSidecars(r.sidecars),
  };
}

function envName(context, configPath) {
  const name = asText(context.display_name) || asText(context.name);
  if (name) return name;
  return configPath.split("/").pop()?.replace(/\.ya?ml$/, "") || "unknown";
}

// cwd is HOME so Orbit never picks up a project's orbit.yaml: the default runtime is then the
// environment selected with `orbit switch`, the one its daemon runs.
async function runOrbit(args) {
  const options = { cwd: home() };
  try {
    return await exec("orbit", args, options);
  } catch {
    return exec(`${home()}/.local/bin/orbit`, args, options);
  }
}

async function orbitJson(args) {
  const ran = await runOrbit(args);
  const text = ran.stdout.trim();
  if (!text) throw new Error(firstLine(ran.stderr) || `orbit ${args.join(" ")} exited ${ran.code}`);
  return asObject(parseJson(text, `orbit ${args.join(" ")}`));
}

function cliError(body) {
  const error = asObject(body.error);
  const message = asText(error.message) || "orbit failed";
  const hint = asText(error.hint);
  return hint ? `${message}\n${hint}` : message;
}

async function orbitData(args) {
  const body = await orbitJson(args);
  if (body.ok === true) return asObject(body.data);
  throw Object.assign(new Error(cliError(body)), { code: asText(asObject(body.error).code) });
}

/** `--instance` for a named instance, `-c` for both, so every call reaches the same runtime. */
export function targetArgs(instance) {
  if (!instance) return [];
  return [...(instance.name ? ["--instance", instance.name] : []), ...(instance.configPath ? ["-c", instance.configPath] : [])];
}

function countsOf(states) {
  if (!states || typeof states !== "object") return { up: null, total: null };
  const entries = Object.entries(states).map(([state, n]) => [state, asNumber(n)]);
  return {
    up: entries.filter(([state]) => state !== "stopped").reduce((sum, [, n]) => sum + n, 0),
    total: entries.reduce((sum, [, n]) => sum + n, 0),
  };
}

function parseDefaultInstance(data) {
  const daemon = asObject(data.daemon);
  const env = asObject(data.environment);
  return {
    name: "", env: asText(env.selected_name), daemon: daemon.running === true, dashboard: asText(daemon.dashboard),
    configPath: asText(env.selected_path), up: null, total: null,
  };
}

function parseNamedInstance(raw) {
  const one = asObject(raw);
  return {
    name: asText(one.name), env: asText(one.environment), daemon: asText(one.state) === "running", dashboard: asText(one.dashboard),
    configPath: asText(one.config_path), ...countsOf(one.resource_states),
  };
}

/** The default runtime (from `inspect`) followed by every named `--instance`; never throws. */
export async function loadInstances() {
  const [inspect, named] = await Promise.all([
    orbitData(["inspect", "--json"]).catch(() => ({})),
    orbitData(["instance", "list", "--json"]).catch(() => ({})),
  ]);
  const instances = asArray(named.instances).map(parseNamedInstance).filter((one) => one.name);
  return [parseDefaultInstance(inspect), ...instances];
}

async function inspectedEnv(instance) {
  const body = await orbitJson([...targetArgs(instance), "inspect", "--json"]).catch(() => ({}));
  return asText(asObject(asObject(body.data).environment).selected_name);
}

/**
 * One `orbit status --json` call. A running daemon makes the panel live and names the environment
 * the way the daemon does; otherwise the configured resources show as stopped, with Orbit's error.
 */
export async function loadStatus(instance) {
  const body = await orbitJson([...targetArgs(instance), "status", "--json"]);
  const data = asObject(body.data);
  const daemon = asObject(data.daemon);
  const resources = sortResources(asArray(data.resources).map((raw) => ({ ...parseResource(raw), state: asText(asObject(raw).state) || "stopped" })));
  // A daemon serving another environment reports the selected config's saved states, not live ones.
  if (body.ok === true && daemon.running === true && daemon.context_mismatch !== true) {
    const context = asObject(daemon.context);
    const managed = asObject(context.managed_selection);
    const configPath = asText(daemon.config_path) || asText(context.config_path);
    const selected = managed.active === false ? asText(managed.name) : "";
    return { source: "daemon", env: envName(context, configPath), configPath, selected, error: "", resources };
  }
  const env = asText(asObject(data.environment).selected_name);
  if (body.ok === true && daemon.context_mismatch === true) {
    return { source: "cli", env: env || "unknown", configPath: "", selected: "", error: "Orbit's daemon is running another environment; switch to it or restart the daemon here.", resources };
  }
  if (body.ok === true) return { source: "cli", env: env || "unknown", configPath: "", selected: "", error: "", resources };
  return { source: "cli", env: env || (await inspectedEnv(instance)) || instance?.env || "unknown", configPath: "", selected: "", error: cliError(body), resources };
}

/** A resource that never produced output reports `logs_unavailable`; the panel shows an empty log. */
export async function loadLogs(instance, name) {
  let data;
  try {
    data = await orbitData([...targetArgs(instance), "logs", name, "--lines", String(LOG_LINES), "--json"]);
  } catch (error) {
    if (error?.code === "logs_unavailable") return [];
    throw error;
  }
  return asArray(data.lines).map((line) => asText(line, JSON.stringify(line)));
}

function parseTraceSummary(raw) {
  const r = asObject(raw);
  return {
    id: asText(r.traceId), rootService: asText(r.rootService, "?"), rootName: asText(r.rootName), start: asNumber(r.startUnixNano),
    durationMs: asNumber(r.durationMs), spanCount: asNumber(r.spanCount), services: asArray(r.services).map((one) => asText(one)),
    status: asText(r.status, "unset"),
  };
}

function parseSpan(raw) {
  const r = asObject(raw);
  return {
    id: asText(r.spanId), parentId: asText(r.parentId), service: asText(r.service, "?"), name: asText(r.name), kind: asText(r.kind),
    start: asNumber(r.startUnixNano), durationMs: asNumber(r.durationMs), status: asText(r.status, "unset"),
    attributes: Object.entries(asObject(r.attributes)).map(([key, value]) => [key, asText(value, JSON.stringify(value))]),
  };
}

export async function loadTraces(instance, includeHealth) {
  const args = [...targetArgs(instance), "trace", "--limit", String(TRACE_LIMIT), ...(includeHealth ? ["--include-health"] : []), "--json"];
  const data = await orbitData(args);
  return asArray(data.traces).map(parseTraceSummary).filter((one) => one.id);
}

/** The trace and the stdout lines that name it, from one `orbit trace <id> --logs --json`. */
export async function loadTrace(instance, id) {
  const body = await orbitJson([...targetArgs(instance), "trace", id, "--logs", "--json"]);
  if (body.ok !== true) throw Object.assign(new Error(cliError(body)), { code: asText(asObject(body.error).code) });
  const data = asObject(body.data);
  const detail = { ...parseTraceSummary(data), spans: asArray(data.spans).map(parseSpan) };
  const unavailable = asArray(body.notices).map(asObject).find((one) => one.code === "stdout_lines_unavailable");
  const logs = unavailable
    ? [`(trace logs unavailable: ${asText(unavailable.message)})`]
    : asArray(data.stdout_lines).map(asObject).map((one) => `[${asText(one.service, "?")}] ${asText(one.line)}`);
  return { detail, logs };
}

/**
 * Orbit waits until the restart or stop is verified, so this resolves with what happened: the
 * message Orbit reports (for example that nothing was running) or the resources it acted on.
 * A `timeout` error means Orbit is still working after ACTION_TIMEOUT, not that it failed.
 */
export async function runAction(instance, action, name) {
  const command = { restart: "restart", stop: "down" }[action];
  if (!command) throw new Error(`unknown action ${action}`);
  const data = await orbitData([...targetArgs(instance), command, name, "--timeout", ACTION_TIMEOUT, "--json"]);
  const message = asText(data.message);
  const affected = asArray(data.resources).map((one) => asText(asObject(one).name, asText(one))).filter(Boolean);
  return { message, affected };
}

/**
 * Stops the instance's containers and services, then its daemon. `daemon stop` alone leaves the
 * containers running with nothing supervising them.
 */
export async function stopDaemon(instance) {
  const target = targetArgs(instance);
  await orbitData([...target, "down", "--json"]);
  await orbitData([...target, "daemon", "stop", "--json"]);
}

async function mapLimit(items, limit, fn) {
  const results = new Array(items.length);
  let next = 0;
  const workers = Array.from({ length: Math.min(limit, items.length) }, async () => {
    while (next < items.length) {
      const at = next++;
      results[at] = await fn(items[at]);
    }
  });
  await Promise.all(workers);
  return results;
}

function sqlFromDiff(target, data) {
  if (data.in_sync === true) return { ...target, status: "in-sync", detail: "In sync" };
  const files = asArray(data.file_changes).length;
  const detail = files
    ? `${files} file${files === 1 ? "" : "s"} changed since publish`
    : `+${asNumber(data.created)} ~${asNumber(data.altered)} -${asNumber(data.dropped)}`;
  return { ...target, status: "changed", detail };
}

/**
 * Each database of the environment's SQL projects with Orbit's own publish record, or null when the
 * environment has no SQL Server workflow. One quick CLI call; nothing is compared yet.
 */
export async function loadSqlList(instance) {
  let data;
  try {
    data = await orbitData([...targetArgs(instance), "sqlserver", "list", "--json"]);
  } catch (error) {
    if (error?.code === "not_configured") return null;
    throw error;
  }
  const records = Array.isArray(data.publish_records) ? data.publish_records.map(asObject) : null;
  const publishedAt = new Map((records ?? [])
    .filter((one) => one.last_publish != null)
    .map((one) => [asText(one.database), Date.parse(asText(asObject(one.last_publish).at)) || null]));
  return asArray(data.projects).map(asObject).flatMap((project) => asArray(project.databases).map((name) => {
    const one = { project: asText(project.name), db: asText(name), publishedAt: publishedAt.get(asText(name)) ?? null };
    if (!records) return { ...one, status: "unknown", detail: "Needs Orbit 0.16.10 or later" };
    if (!publishedAt.has(one.db)) return { ...one, status: "not-published", detail: "Not published" };
    return { ...one, status: "pending", detail: "Comparing with the project..." };
  }));
}

/**
 * Compares each published database with its project and reports every result as it arrives.
 * A database Orbit never published is skipped: the diff would run a full SqlPackage comparison
 * (seconds each) only to say it differs.
 */
export async function diffSql(instance, rows, onRow) {
  const target = targetArgs(instance);
  const pending = rows.filter((one) => one.status === "pending");
  await mapLimit(pending, SQL_DIFF_CONCURRENCY, async (one) => {
    let row;
    try {
      row = sqlFromDiff(one, await orbitData([...target, "sqlserver", "diff", one.db, "--json"]));
    } catch (error) {
      row = { ...one, status: "unknown", detail: errorText(error) };
    }
    onRow(row);
  });
}
