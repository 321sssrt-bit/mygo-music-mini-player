const platforms = ["windows", "linux"];
const architectures = ["amd64", "arm64"];
const binary = platforms.includes(host.platform) && architectures.includes(host.arch)
  ? `bin/mini-player-${host.platform}-${host.arch}${host.platform === "windows" ? ".exe" : ""}`
  : null;
const permissionHelp = "In Settings → Plugins, allow Mini Player to run programs.";
const startError = "Mini Player could not start. Check that the plugin package matches your platform.";
const actionError = "Mini Player could not complete the action.";
const macHelp = "Mini Player is unavailable on macOS. MyGo Music already includes the Notch Player, and macOS 15.4+ restricts third-party access to system media sessions.";
const platformHelp = "Mini Player supports Windows and Linux on amd64 and arm64.";
const stateLabels = { starting: "Starting", waiting: "Waiting for MyGo Music" };

let directProcess;
let pending = Promise.resolve();

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

function page(status) {
  const state = stateLabels[status.state] || (status.visible ? "Running · Visible" : "Running · Hidden");
  const policy = status.displayPolicy === "pinned" ? "Pinned Display" : "Auto-Link";
  const links = [
    status.visible
      ? { title: host.t("Hide Mini Player"), page: "hide" }
      : { title: host.t("Show Mini Player now"), page: "show" },
    { title: host.t("Reset position to screen center"), page: "reset-position" },
  ];
  return {
    title: host.t("Mini Player"),
    subtitle: host.t(state) + " · " + host.t(policy),
    sections: [{ links }],
  };
}

export function home() {
  return browse({});
}

export async function browse({ page: action }) {
  const title = host.t("Mini Player");
  if (!binary) return { title, subtitle: host.t(host.platform === "darwin" ? macHelp : platformHelp) };
  return serial(async () => {
    let current;
    try {
      current = await running();
    } catch (error) {
      return { title, subtitle: error.message };
    }
    if (["show", "hide", "reset-position"].includes(action)) {
      try {
        current.status = await request(current.endpoint, "/" + action);
      } catch {
        return { ...page(current.status), description: host.t(actionError) };
      }
    }
    return page(current.status);
  });
}
