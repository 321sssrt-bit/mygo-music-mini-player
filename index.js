const platforms = ["windows", "linux"];
const architectures = ["amd64", "arm64"];
const binary = platforms.includes(host.platform) && architectures.includes(host.arch)
  ? `bin/mini-player-${host.platform}-${host.arch}${host.platform === "windows" ? ".exe" : ""}`
  : null;
const permissionHelp = "In Settings → Plugins, choose Allow Programs for Mini Player.";
const startError = "Mini Player could not start. Check that the plugin package matches your platform.";
const actionError = "Mini Player could not complete the action.";
const macHelp = "Mini Player is unavailable on macOS. MyGo Music already includes the Notch Player, and macOS 15.4+ restricts third-party access to system media sessions.";
const platformHelp = "Mini Player supports Windows and Linux on amd64 and arm64.";
const stateLabels = { starting: "Starting", waiting: "Waiting for MyGo Music" };

let directProcess;
let pending = Promise.resolve();
const pageEpoch = Date.now();
let pageSequence = pageEpoch;
const usedLinks = new Set();

function serial(run) {
  const result = pending.then(run);
  pending = result.catch(() => {});
  return result;
}

function command(action, flags = []) {
  try {
    return host.spawn(binary, [action, "--data-dir", host.dataDir, ...flags]);
  } catch (error) {
    if (String(error).includes("hasn't allowed this plugin to start programs")) {
      throw Object.assign(new Error(host.t(permissionHelp)), { code: "setup" });
    }
    throw new Error(host.t(startError));
  }
}

async function discovery(process) {
  try {
    const { address, token } = JSON.parse(await process.readLine());
    const port = typeof address === "string" && /^http:\/\/127\.0\.0\.1:([1-9]\d{0,4})$/.exec(address);
    if (port && Number(port[1]) <= 65535 && typeof token === "string" && /^[a-f0-9]{64}$/.test(token)) {
      return { address, token };
    }
  } catch {
    // Missing or corrupt discovery is the same as an absent endpoint.
  }
  return null;
}

async function request(endpoint, path) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 5000);
  try {
    const response = await fetch(endpoint.address + path, {
      method: path === "/status" ? "GET" : "POST",
      headers: { Authorization: "Bearer " + endpoint.token },
      redirect: "error",
      signal: controller.signal,
    });
    if (!response.ok) throw new Error(host.t(actionError));
    return await response.json();
  } finally {
    clearTimeout(timeout);
  }
}

async function start(guard) {
  const process = command("serve", guard ? ["--guard", "--detach"] : []);
  const endpoint = await discovery(process);
  if (endpoint) {
    if (!guard) directProcess = process;
    try {
      return { endpoint, status: await request(endpoint, "/status") };
    } catch {
      // The reported process may have exited before we reached it.
    }
  }
  throw new Error(host.t(startError));
}

async function running() {
  const endpoint = await discovery(command("discover"));
  if (endpoint) {
    try {
      return { endpoint, status: await request(endpoint, "/status") };
    } catch {
      // A forced exit can leave an old control.json behind.
    }
  }
  return start(host.setting("guardMode") === "true");
}

export async function account() {
  if (binary) await serial(running);
  return null;
}

export async function setSetting({ key, value }) {
  if (!binary) return;
  return serial(async () => {
    try {
      const { endpoint } = await running();
      await request(endpoint, "/refresh");
    } catch (error) {
      // The host already saved the setting and will pass it to the next launch.
      if (error.code === "setup") return;
      throw error;
    }
    if (key === "guardMode" && value === "true" && directProcess) {
      const process = directProcess;
      directProcess = undefined;
      try {
        process.kill();
      } catch {
        // A duplicate serve may already have exited after reporting the owner.
      }
      await process.wait();
      await start(true);
    }
  });
}

function page(status, onHome, description) {
  const state = stateLabels[status.state] || (status.visible ? "Running · Visible" : "Running · Hidden");
  const policy = status.displayPolicy === "pinned" ? "Pinned Display" : "Auto-Link";
  const subtitle = host.t(state) + " · " + host.t(policy);
  // The host caches each Browse page ID, so the next click needs a fresh ID.
  const id = ++pageSequence;
  const links = [
    status.visible
      ? { title: host.t("Hide Mini Player"), page: `hide/${id}` }
      : { title: host.t("Show Mini Player now"), page: `show/${id}` },
    { title: host.t("Reset position to screen center"), page: `reset-position/${id}` },
  ];
  return {
    title: host.t("Mini Player"),
    subtitle,
    description,
    // The host uses a greeting in Home and renders plugin text in its sections.
    sections: [onHome ? { title: subtitle, links } : { links }],
  };
}

function notice(subtitle, onHome) {
  const title = host.t("Mini Player");
  return onHome ? { title, subtitle, sections: [{ empty: subtitle }] } : { title, subtitle };
}

async function consolePage(onHome, link) {
  if (!binary) return notice(host.t(host.platform === "darwin" ? macHelp : platformHelp), onHome);
  return serial(async () => {
    let current;
    try {
      current = await running();
    } catch (error) {
      return notice(error.message, onHome);
    }
    const match = /^(show|hide|reset-position)\/(\d+)$/.exec(link);
    const number = match && Number(match[2]);
    if (number > pageEpoch && number <= pageSequence && !usedLinks.has(link)) {
      // Refreshing an already opened Browse page should just read live status.
      usedLinks.add(link);
      try {
        current.status = await request(current.endpoint, "/" + match[1]);
      } catch {
        return page(current.status, onHome, host.t(actionError));
      }
    }
    return page(current.status, onHome);
  });
}

export function home() {
  return consolePage(true);
}

export function browse({ page: id }) {
  return consolePage(false, id);
}
