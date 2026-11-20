import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { setTimeout as delay } from "node:timers/promises";
import { createContext, SourceTextModule } from "node:vm";

const [fixture, temp] = process.argv.slice(2);
const json = async (path) => JSON.parse(await readFile(path, "utf8"));
const manifest = await json("plugin.json");
const source = await readFile(manifest.main, "utf8");
const locales = { en: await json("locales/en.json"), "zh-CN": await json("locales/zh-CN.json") };
const binaries = {
  "windows-amd64": "bin/mini-player-windows-amd64.exe",
  "windows-arm64": "bin/mini-player-windows-arm64.exe",
  "linux-amd64": "bin/mini-player-linux-amd64",
  "linux-arm64": "bin/mini-player-linux-arm64",
};
assert.equal(manifest.id, "mini-player");
assert.equal(manifest.minAppVersion, "0.1.25");
assert.deepEqual(manifest.permissions, { process: Object.values(binaries) });
assert.deepEqual(manifest.settings.map(({ key, type, options }) => [key, type, options?.map(({ value }) => value)]), [
  ["displayPolicy", "select", ["auto", "pinned"]],
  ["smokeOverride", "select", ["follow", "on", "off"]],
  ["guardMode", "boolean", undefined],
  ["hotkey", "text", undefined],
]);
assert.deepEqual(Object.keys(locales.en).sort(), Object.keys(locales["zh-CN"]).sort());
for (const { label, help, options = [] } of manifest.settings) {
  for (const text of [label, help, ...options.map((option) => option.label)]) {
    assert.ok(locales["zh-CN"][text], text);
  }
}

async function sandbox(platform, arch, granted = true, language = "en") {
  const dataDir = join(temp, `${platform} ${arch} ${language} ${granted}`);
  await mkdir(dataDir);
  const settings = {};
  const launches = [];
  const requests = [];
  const requestsBeforeKill = [];
  const children = [];
  let detached = false;
  const context = createContext({
    AbortController,
    setTimeout, clearTimeout,
    async fetch(url, options) {
      assert.ok(granted, "ungranted plugin must not send a request");
      assert.match(url, /^http:\/\/127\.0\.0\.1:\d+\//);
      assert.match(options.headers.Authorization, /^Bearer [a-f0-9]{64}$/);
      assert.equal(options.redirect, "error");
      requests.push([options.method, new URL(url).pathname]);
      return fetch(url, options);
    },
    host: {
      platform, arch, dir: process.cwd(), dataDir,
      setting: (key) => settings[key] ?? "",
      language: () => language,
      t(text) {
        assert.ok(locales[language][text], `missing ${language} translation for ${text}`);
        return locales[language][text];
      },
      spawn(command, args) {
        assert.equal(command, binaries[`${platform}-${arch}`]);
        assert.ok(manifest.permissions.process.includes(command));
        assert.equal(args[1], "--data-dir");
        assert.equal(args[2], dataDir);
        if (!granted) throw new Error("the listener hasn't allowed this plugin to start programs");
        launches.push(Array.from(args));
        const child = spawn(fixture, args, { stdio: ["pipe", "pipe", "ignore"], windowsHide: true });
        children.push(child);
        if (args.includes("--detach")) detached = true;
        const done = new Promise((resolve, reject) => {
          child.once("error", reject);
          child.once("exit", (code) => resolve(code ?? -1));
        });
        const lines = createInterface({ input: child.stdout })[Symbol.asyncIterator]();
        return {
          pid: child.pid,
          async readLine() {
            const line = await lines.next();
            return line.done ? null : line.value;
          },
          kill() {
            requestsBeforeKill.push(requests.at(-1));
            if (!child.kill()) throw new Error("process already exited");
          },
          wait: () => done,
        };
      },
    },
  });
  const module = new SourceTextModule(source, { context });
  await module.link(() => { throw new Error("host entry must be a single module"); });
  await module.evaluate();
  async function save(key, value) {
    settings[key] = value;
    await writeFile(join(dataDir, "settings.json"), JSON.stringify(settings));
    await module.namespace.setSetting({ key, value });
  }
  async function close() {
    await writeFile(join(dataDir, "stop"), "");
    if (detached) {
      for (let i = 0; i < 100; i++) {
        if (await readFile(join(dataDir, "control.json")).catch(() => null) === null) break;
        await delay(10);
      }
    }
    for (const child of children) {
      if (child.exitCode === null && child.signalCode === null) {
        const stopped = new Promise((resolve) => child.once("exit", resolve));
        child.kill();
        await stopped;
      }
    }
  }
  return { plugin: module.namespace, dataDir, launches, requests, requestsBeforeKill, save, close };
}

let stale;
for (const target of Object.keys(binaries)) {
  const s = await sandbox(...target.split("-"));
  try {
    // Reject a discovery address outside the exact loopback format.
    if (target === "windows-amd64") {
      await writeFile(join(s.dataDir, "control.json"), JSON.stringify({
        address: "http://localhost:1", token: "a".repeat(64),
      }));
    }
    if (target === "windows-arm64") {
      await writeFile(join(s.dataDir, "control.json"), JSON.stringify(stale));
    }
    const { plugin, dataDir, launches, requests, save } = s;
    const [account, home] = await Promise.all([plugin.account(), plugin.home()]);
    assert.equal(account, null);
    assert.equal(home.subtitle, "Running · Hidden · Auto-Link");
    assert.equal(home.sections[0].title, home.subtitle);
    assert.deepEqual(launches.filter(([action]) => action === "serve"), [["serve", "--data-dir", dataDir]]);
    const visited = new Map();
    async function click(page, row) {
      const id = page.sections[0].links[row].page;
      if (!visited.has(id)) visited.set(id, await plugin.browse({ page: id }));
      return visited.get(id);
    }
    const shown = await click(home, 0);
    assert.equal(shown.subtitle, "Running · Visible · Auto-Link");
    const hidden = await click(shown, 0);
    assert.equal(hidden.subtitle, "Running · Hidden · Auto-Link");
    assert.equal((await plugin.browse({ page: home.sections[0].links[0].page })).subtitle, hidden.subtitle);
    const shownAgain = await click(hidden, 0);
    assert.equal(shownAgain.subtitle, "Running · Visible · Auto-Link");
    const reset = await click(shownAgain, 1);
    assert.equal(reset.subtitle, "Running · Visible · Auto-Link");
    assert.equal((await click(reset, 1)).subtitle, reset.subtitle);
    await save("displayPolicy", "pinned");
    assert.equal((await plugin.home()).subtitle, "Running · Visible · Pinned Display");
    assert.deepEqual(requests.filter(([method]) => method === "POST"), [
      ["POST", "/show"], ["POST", "/hide"], ["POST", "/show"],
      ["POST", "/reset-position"], ["POST", "/reset-position"], ["POST", "/refresh"],
    ]);
    assert.equal(await readFile(join(dataDir, "actions"), "utf8"), "/show\n/hide\n/show\n/reset-position\n/reset-position\n/refresh\n");
    console.log(`${target}: show → hide → show again → reset twice → pinned`);
    if (target === "windows-amd64") {
      const initial = await json(join(dataDir, "control.json"));
      await save("guardMode", "true");
      assert.deepEqual(launches.filter(([action]) => action === "serve").at(-1), [
        "serve", "--data-dir", dataDir, "--guard", "--detach",
      ]);
      const waiting = await json(join(dataDir, "control.json"));
      assert.ok(waiting.token !== initial.token, "guard launch must replace the direct process");
      assert.deepEqual(s.requestsBeforeKill, [["POST", "/refresh"]]);
      assert.equal((await plugin.home()).subtitle, "Waiting for MyGo Music · Pinned Display");
      stale = waiting;
      const count = launches.filter(([action]) => action === "serve").length;
      await save("guardMode", "false");
      await save("guardMode", "true");
      assert.equal(launches.filter(([action]) => action === "serve").length, count);
      console.log("guard: active → waiting, existing guard reused");
    }
  } finally {
    await s.close();
  }
}

for (const language of Object.keys(locales)) {
  const denied = await sandbox("windows", "amd64", false, language);
  try {
    const home = await denied.plugin.home();
    assert.match(home.subtitle, /Allow Programs|允许启动程序/);
    assert.equal(home.sections[0].empty, home.subtitle);
    await assert.rejects(denied.plugin.account(), { code: "setup" });
    await denied.save("guardMode", "true");
    assert.equal(denied.requests.length, 0);
  } finally {
    await denied.close();
  }
  const mac = await sandbox("darwin", "arm64", true, language);
  try {
    assert.equal(await mac.plugin.account(), null);
    const home = await mac.plugin.home();
    assert.match(home.subtitle, /Notch Player.*macOS 15\.4\+/);
    assert.equal(home.sections[0].empty, home.subtitle);
    assert.equal((await mac.plugin.browse({ page: "show/1" })).subtitle, home.subtitle);
    await mac.save("guardMode", "true");
    assert.equal(mac.launches.length, 0);
    assert.equal(mac.requests.length, 0);
    console.log(`${language}: permission guidance and macOS fallback`);
  } finally {
    await mac.close();
  }
}
