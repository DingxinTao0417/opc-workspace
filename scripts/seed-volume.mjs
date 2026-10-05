import { spawn, spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// Volume seed and API baseline for the large-data acceptance items.
//
// It starts its own Sidecar on a dedicated data directory, creates a fixed
// volume of clients, projects, tasks and inbox items through the public API,
// measures the list endpoints the pages use, and leaves the data directory in
// place so the same data can be opened in the real UI.
//
// The timings it prints describe one machine at one moment. They are a
// baseline to compare against, not a gate and not a substitute for observing
// the UI: this script is not part of `pnpm check`.

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = dirname(scriptsDir);
const goCommand = process.platform === "win32" ? "go.exe" : "go";

const PROFILES = {
  "1k": { clients: 20, projects: 50, tasks: 1_000, inbox: 1_000 },
  "10k": { clients: 50, projects: 200, tasks: 10_000, inbox: 10_000 },
};
const SAMPLES = 30;
const WARMUPS = 3;
const WRITERS = 4;

function option(name, fallback) {
  const index = process.argv.indexOf(`--${name}`);
  return index >= 0 ? process.argv[index + 1] : fallback;
}

const profileName = option("profile", "1k");
const profile = PROFILES[profileName];
if (!profile) {
  console.error(
    `Unknown profile "${profileName}". Use one of: ${Object.keys(PROFILES).join(", ")}.`,
  );
  process.exit(2);
}
const dataDir = resolve(
  option("data-dir", join(repoRoot, ".local", "perf-data", profileName)),
);
const port = Number(option("port", "9877"));
const outPath = option("out", null);
const token = `seed-volume-${randomUUID()}`;
const base = `http://127.0.0.1:${port}`;
const origin = "http://127.0.0.1:1420";
const databasePath = join(dataDir, "opc-workspace.db");
const accessLog = join(dataDir, "logs", "opc-sidecar.log");

// Exact row counts are the point of a volume profile, so an existing database
// is never topped up and never deleted from here.
if (existsSync(databasePath)) {
  console.error(
    `${databasePath} already exists. Remove that directory yourself or pass --data-dir with a new one.`,
  );
  process.exit(2);
}
for (const name of ["artifacts", "invoices", "backups", "logs"]) {
  mkdirSync(join(dataDir, name), { recursive: true });
}

// A small deterministic generator, so two runs create the same titles.
let state = 0x2f6e2b1;
function random() {
  state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
  return state / 0x100000000;
}
const pick = (items) => items[Math.floor(random() * items.length)];

const today = new Date();
function day(offset) {
  const d = new Date(today);
  d.setDate(d.getDate() + offset);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}
function instant(offset, hour) {
  const d = new Date(today);
  d.setDate(d.getDate() + offset);
  d.setHours(hour, 0, 0, 0);
  return d.toISOString();
}

async function call(method, path, body) {
  const response = await fetch(`${base}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      Origin: origin,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok) {
    throw new Error(
      `${method} ${path} -> ${response.status} ${text.slice(0, 200)}`,
    );
  }
  return { bytes: Buffer.byteLength(text), json: JSON.parse(text) };
}

async function createMany(label, count, build) {
  const created = new Array(count);
  let next = 0;
  let done = 0;
  const started = Date.now();
  async function worker() {
    while (next < count) {
      const index = next++;
      const { path, body } = build(index);
      created[index] = (await call("POST", path, body)).json.data;
      done++;
      if (done % 1000 === 0) console.log(`  ${label}: ${done}/${count}`);
    }
  }
  await Promise.all(Array.from({ length: WRITERS }, worker));
  const seconds = (Date.now() - started) / 1000;
  console.log(`  ${label}: ${count} created in ${seconds.toFixed(1)} s`);
  return created;
}

function quantile(sorted, q) {
  return sorted[Math.min(sorted.length - 1, Math.ceil(q * sorted.length) - 1)];
}

// The Sidecar logs how long it spent on every request. On Windows the client
// clock around a loopback request moves in steps of roughly 15 ms, so the
// server's own figure is reported next to the client-observed one.
async function serverDurations(pathname, count) {
  await new Promise((r) => setTimeout(r, 200));
  const needle = `method=GET path=${pathname} `;
  const durations = readFileSync(accessLog, "utf8")
    .split("\n")
    .filter((line) => line.includes(needle))
    .slice(-count)
    .map((line) => Number(/duration_ms=(\d+)/.exec(line)?.[1]))
    .filter(Number.isFinite)
    .sort((a, b) => a - b);
  if (durations.length !== count) return { serverP50: null, serverP95: null };
  return {
    serverP50: quantile(durations, 0.5),
    serverP95: quantile(durations, 0.95),
  };
}

async function measure(label, path) {
  for (let i = 0; i < WARMUPS; i++) await call("GET", path);
  const timings = [];
  let bytes = 0;
  for (let i = 0; i < SAMPLES; i++) {
    const started = performance.now();
    bytes = (await call("GET", path)).bytes;
    timings.push(performance.now() - started);
  }
  timings.sort((a, b) => a - b);
  return {
    label,
    path,
    p50: quantile(timings, 0.5),
    p95: quantile(timings, 0.95),
    max: timings[timings.length - 1],
    kilobytes: bytes / 1024,
    ...(await serverDurations(new URL(path, base).pathname, SAMPLES)),
  };
}

// The Today page reads every unscheduled active task, 100 per request, and
// manual ordering reads whole lists the same way; time one such full read.
async function measureUnscheduledTaskPages() {
  const timings = [];
  let pages = 0;
  for (let run = 0; run < 3; run++) {
    const started = performance.now();
    pages = 0;
    for (let page = 1; ; page++) {
      const { json } = await call(
        "GET",
        `/api/v1/tasks?page=${page}&page_size=100&status=active&planned_state=unscheduled`,
      );
      pages++;
      const total = json.meta?.total ?? 0;
      if (page * 100 >= total) break;
    }
    timings.push(performance.now() - started);
  }
  timings.sort((a, b) => a - b);
  return {
    label: `未排期任务全量读取（${pages} 页 × 100，整轮耗时，3 轮）`,
    path: "/api/v1/tasks?page=N&page_size=100&status=active&planned_state=unscheduled",
    p50: timings[1],
    p95: timings[2],
    max: timings[2],
    kilobytes: Number.NaN,
    // Per request, over every page of the three rounds.
    ...(await serverDurations("/api/v1/tasks", pages * 3)),
  };
}

function stop(child) {
  if (!child.pid) return;
  if (process.platform === "win32") {
    spawnSync("taskkill", ["/pid", String(child.pid), "/T", "/F"], {
      stdio: "ignore",
    });
  } else {
    child.kill("SIGTERM");
  }
}

async function main() {
  console.log(`profile ${profileName}: ${JSON.stringify(profile)}`);
  console.log(`data directory: ${dataDir}`);
  const sidecar = spawn(
    goCommand,
    [
      "-C",
      join(repoRoot, "services", "sidecar"),
      "run",
      "./cmd/server",
      "--db",
      databasePath,
      "--artifacts",
      join(dataDir, "artifacts"),
      "--invoices",
      join(dataDir, "invoices"),
      "--backups",
      join(dataDir, "backups"),
      "--logs",
      join(dataDir, "logs"),
      "--port",
      String(port),
      "--dev",
    ],
    {
      env: {
        ...process.env,
        OPC_SESSION_TOKEN: token,
        OPC_ALLOWED_ORIGINS: origin,
      },
      stdio: "ignore",
    },
  );
  let exited = false;
  sidecar.once("exit", () => {
    exited = true;
  });
  try {
    const deadline = Date.now() + 180_000;
    for (;;) {
      if (exited) throw new Error("The Sidecar exited before it became ready.");
      try {
        await call("GET", "/health");
        break;
      } catch {
        if (Date.now() > deadline)
          throw new Error("The Sidecar did not become ready in 180 s.");
        await new Promise((r) => setTimeout(r, 500));
      }
    }

    console.log("seeding:");
    const clients = await createMany("clients", profile.clients, (i) => ({
      path: "/api/v1/clients",
      body: {
        name: `容量客户 ${String(i + 1).padStart(3, "0")}`,
        contact_name: `联系人 ${i + 1}`,
        email: `volume-${i + 1}@example.test`,
        phone: null,
        notes: "容量验收用数据",
        status: "active",
      },
    }));
    const projects = await createMany("projects", profile.projects, (i) => ({
      path: "/api/v1/projects",
      body: {
        name: `容量项目 ${String(i + 1).padStart(4, "0")}`,
        description: "容量验收用数据",
        client_id: clients[i % clients.length].id,
        start_date: day(-30),
        due_date: day(10 + (i % 90)),
        amount_minor: 100000 + i * 1000,
        color: pick(["#5e6ad2", "#35b9a6", "#e5484d", "#f5a524"]),
      },
    }));
    const verbs = ["梳理", "确认", "编写", "整理", "检查", "发布", "回访"];
    const nouns = ["信息架构", "视觉规范", "接口文档", "周报", "客户反馈"];
    await createMany("tasks", profile.tasks, (i) => ({
      path: "/api/v1/tasks",
      body: {
        title: `${pick(verbs)}${pick(nouns)} ${String(i + 1).padStart(5, "0")}`,
        description: "容量验收用数据",
        kind: "work",
        priority: pick(["P0", "P1", "P2", "P3"]),
        project_id: i % 5 === 0 ? null : projects[i % projects.length].id,
        parent_task_id: null,
        completion_criteria: "",
        review_policy: "none",
        tag_ids: [],
        due_date: instant((i % 60) - 10, 18),
        planned_date: i % 10 === 0 ? day(i % 3) : null,
        estimated_minutes: 30 + (i % 8) * 15,
        manual_order: null,
      },
    }));
    await createMany("inbox items", profile.inbox, (i) => ({
      path: "/api/v1/inbox-items",
      body: {
        title: `容量事项 ${String(i + 1).padStart(5, "0")}：${pick(nouns)}`,
        summary: "容量验收用数据",
        priority: pick(["P1", "P2", "P3"]),
        due_at: instant(1 + (i % 30), 10),
      },
    }));

    console.log("measuring:");
    const lastTaskPage = Math.ceil(profile.tasks / 100);
    const lastInboxPage = Math.ceil(profile.inbox / 50);
    const results = [
      await measure("任务列表首页", "/api/v1/tasks?page=1&page_size=100"),
      await measure(
        "任务列表末页",
        `/api/v1/tasks?page=${lastTaskPage}&page_size=100`,
      ),
      await measure(
        "任务搜索",
        `/api/v1/tasks?page=1&page_size=100&q=${encodeURIComponent("接口文档")}`,
      ),
      await measureUnscheduledTaskPages(),
      await measure("项目列表首页", "/api/v1/projects?page=1&page_size=50"),
      await measure("客户列表首页", "/api/v1/clients?page=1&page_size=50"),
      await measure(
        "收件箱首页",
        "/api/v1/inbox-items?view=inbox&page=1&page_size=50",
      ),
      await measure(
        "收件箱末页",
        `/api/v1/inbox-items?view=inbox&page=${lastInboxPage}&page_size=50`,
      ),
      await measure(
        "全局搜索",
        `/api/v1/search?q=${encodeURIComponent("容量")}&page=1&page_size=20`,
      ),
    ];
    const ms = (value) => value.toFixed(1).padStart(8);
    console.log("");
    const server = (value) => (value === null ? "—" : String(value));
    console.log(
      `| 请求 | 客户端 p50 ms | 客户端 p95 ms | 服务端 p50 ms | 服务端 p95 ms | 响应 KiB | (${SAMPLES} 次，预热 ${WARMUPS} 次) |`,
    );
    console.log("| --- | ---: | ---: | ---: | ---: | ---: | --- |");
    for (const r of results) {
      const size = Number.isNaN(r.kilobytes) ? "—" : r.kilobytes.toFixed(1);
      console.log(
        `| ${r.label} | ${ms(r.p50)} | ${ms(r.p95)} | ${server(r.serverP50)} | ${server(r.serverP95)} | ${size} | \`${r.path}\` |`,
      );
    }
    if (outPath) {
      writeFileSync(
        resolve(outPath),
        `${JSON.stringify({ profile: profileName, counts: profile, node: process.version, platform: `${process.platform}-${process.arch}`, results }, null, 2)}\n`,
      );
      console.log(`\nwrote ${resolve(outPath)}`);
    }
    console.log(
      `\nClient times are observed by this script (Node fetch over loopback); server times are the Sidecar's own duration_ms from ${accessLog}.`,
    );
    console.log(
      `\nThe data stays in ${dataDir}. To open it in the app, set OPC_DEV_DATA_DIR to that path and run \`pnpm dev\` (or \`pnpm dev:web\`).`,
    );
  } finally {
    stop(sidecar);
  }
}

main().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
