#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const rootDir = path.resolve(__dirname, "../..");
const outDir = path.join(rootDir, "public", "stardew");
const force = process.argv.includes("--force");

const RAW_BASE = "https://raw.githubusercontent.com/howbit/stardew-monitor/main";
const CDN_BASE = "https://cdn.jsdelivr.net/gh/howbit/stardew-monitor@main";

const SEASONS = ["spring", "summer", "fall", "winter"];
const PHASES = ["day", "night"];
const CABINS = [
  "farmhouse-3",
  "big-shed-3",
  "shed-3",
  "junimo-3",
  "mill-3",
  "wizard-tower-3",
  "witch-hut-3",
  "deluxe-coop-3",
  "slime-hutch-3",
  "manor-house-3",
  "fish-shop-3",
  "island-farmhouse-3",
  "log-3",
  "stone-3",
  "plank-3",
  "beach-3",
];
const STATS = ["farm-computer", "scarecrow", "seedling", "wilted-plant"];

const tasks = [];

for (const s of SEASONS) {
  for (const p of PHASES) {
    tasks.push({
      src: `assets/landscape/native-spring-estate/${s}-${p}.png`,
      dest: path.join(outDir, "landscape", `${s}-${p}.png`),
    });
    tasks.push({
      src: `assets/music/${s}-${p}.ogg`,
      dest: path.join(outDir, "music", `${s}-${p}.ogg`),
    });
  }
}

tasks.push({
  src: "assets/music/manifest.json",
  dest: path.join(outDir, "music", "manifest.json"),
});

for (const c of CABINS) {
  tasks.push({
    src: `assets/ui/estate/${c}.png`,
    dest: path.join(outDir, "cabins", `${c}.png`),
  });
  tasks.push({
    src: `assets/ui/estate/windows/${c}.png`,
    dest: path.join(outDir, "windows", `${c}.png`),
  });
}

for (const st of STATS) {
  tasks.push({
    src: `reviews/01-stats/assets/${st}.png`,
    dest: path.join(outDir, "stats", `${st}.png`),
  });
}

async function downloadFile(task) {
  if (!force && fs.existsSync(task.dest) && fs.statSync(task.dest).size > 100) {
    return "skipped";
  }
  fs.mkdirSync(path.dirname(task.dest), { recursive: true });
  const urls = [`${RAW_BASE}/${task.src}`, `${CDN_BASE}/${task.src}`];
  let lastErr;
  for (const url of urls) {
    try {
      const res = await fetch(url, { signal: AbortSignal.timeout(30000) });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const buf = Buffer.from(await res.arrayBuffer());
      if (buf.length < 50) throw new Error("Empty payload");
      fs.writeFileSync(task.dest, buf);
      return "downloaded";
    } catch (err) {
      lastErr = err;
    }
  }
  throw new Error(`Failed ${task.src}: ${lastErr?.message || lastErr}`);
}

async function main() {
  let downloaded = 0;
  let skipped = 0;
  const failed = [];
  const queue = [...tasks];
  const workers = Array.from({ length: 6 }, async () => {
    while (queue.length > 0) {
      const task = queue.shift();
      if (!task) break;
      try {
        const res = await downloadFile(task);
        if (res === "downloaded") downloaded++;
        else skipped++;
      } catch (err) {
        failed.push(err.message);
      }
    }
  });
  await Promise.all(workers);
  console.log(
    `[stardew:assets] total=${tasks.length} downloaded=${downloaded} skipped=${skipped} failed=${failed.length}`,
  );
  if (failed.length > 0) {
    for (const msg of failed) console.error(`  - ${msg}`);
    process.exitCode = 1;
  }
}

void main();
