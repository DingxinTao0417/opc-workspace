// Opt-in Windows/WebView2 integration check. Start a desktop development build
// with WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=9223 first.
// Uses only loopback fixture pages, and closes only the tabs it creates.
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { setTimeout as delay } from "node:timers/promises";

const debugOrigin = "http://127.0.0.1:9223";
const clients = [];
const created = new Set();
let originalActive = null;
let main;

async function targets() {
  return (await fetch(`${debugOrigin}/json/list`)).json();
}

async function connect(target) {
  const socket = new WebSocket(target.webSocketDebuggerUrl);
  const pending = new Map();
  let id = 0;
  await new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, { once: true });
    socket.addEventListener("error", reject, { once: true });
  });
  socket.addEventListener("message", (event) => {
    const message = JSON.parse(String(event.data));
    if (!message.id) return;
    const handler = pending.get(message.id);
    if (!handler) return;
    pending.delete(message.id);
    clearTimeout(handler.timeout);
    if (message.error) handler.reject(new Error(message.error.message));
    else handler.resolve(message.result);
  });
  const client = {
    send(method, params = {}) {
      return new Promise((resolve, reject) => {
        const next = ++id;
        const timeout = setTimeout(() => {
          pending.delete(next);
          reject(new Error(`CDP timed out: ${method}`));
        }, 15000);
        pending.set(next, { resolve, reject, timeout });
        socket.send(JSON.stringify({ id: next, method, params }));
      });
    },
    async evaluate(expression) {
      const result = await this.send("Runtime.evaluate", {
        expression,
        awaitPromise: true,
        returnByValue: true,
      });
      if (result.exceptionDetails) {
        throw new Error(
          result.exceptionDetails.exception?.description ?? "Evaluation failed",
        );
      }
      return result.result.value;
    },
    close() {
      socket.close();
    },
  };
  clients.push(client);
  return client;
}

const invoke = (command, args = {}) =>
  main.evaluate(
    `window.__TAURI_INTERNALS__.invoke(${JSON.stringify(command)}, ${JSON.stringify(args)})`,
  );

async function until(check, message) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const value = await check();
    if (value) return value;
    await delay(100);
  }
  throw new Error(`Timed out: ${message}`);
}

const fixture = createServer((request, response) => {
  const page = request.url?.split("?")[0] ?? "/one";
  if (page === "/redirect") {
    response.writeHead(302, { Location: "/two" });
    response.end();
    return;
  }
  response.writeHead(200, {
    "Content-Type": "text/html; charset=utf-8",
    "X-Frame-Options": "DENY",
    "Content-Security-Policy": "frame-ancestors 'none'",
  });
  response.end(`<!doctype html><html><head><title>Browser fixture ${page}</title></head>
    <body style="font:24px system-ui;padding:32px;background:#eef5ff">
    <h1>Native Chromium ${page}</h1><p>This page refuses iframe embedding.</p>
    <a id="next" href="/two">Next page</a><br>
    <a id="popup" href="/popup" target="_blank" onclick="document.getElementById('click-state').textContent='Popup link clicked'">Open new browser tab</a>
    <p id="click-state">No popup click yet</p>
    <button onclick="window.open('/popup-script','_blank');document.getElementById('click-state').textContent='window.open requested'">Open scripted tab</button>
    </body></html>`);
});

try {
  await new Promise((resolve) => fixture.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${fixture.address().port}`;
  if (process.argv.includes("--serve-fixtures")) {
    console.log(`Manual native browser fixture: ${origin}/one`);
    await new Promise((resolve) => {
      process.once("SIGINT", resolve);
      process.once("SIGTERM", resolve);
    });
  } else {
    const list = await targets();
    const app = list.find(
      (target) =>
        target.type === "page" &&
        /^http:\/\/127\.0\.0\.1:1420\//.test(target.url),
    );
    assert.ok(app, "Desktop development WebView must be running on port 1420");
    main = await connect(app);
    const before = await invoke("browser_snapshot");
    originalActive = before.activeTabId;
    assert.ok(before.tabs.length <= 8, "Leave room for isolated smoke tabs");

    let state = await invoke("browser_create_tab", { url: `${origin}/one` });
    const first = state.activeTabId;
    created.add(first);
    await invoke("browser_set_layout", {
      bounds: { x: 400, y: 180, width: 500, height: 400 },
    });
    await until(
      async () =>
        (await invoke("browser_snapshot")).tabs.find(
          (tab) =>
            tab.id === first &&
            !tab.loading &&
            tab.title === "Browser fixture /one",
        ),
      "first page/title",
    );
    const firstTarget = await until(
      async () =>
        (await targets()).find((target) => target.url === `${origin}/one`),
      "native page target",
    );
    const page = await connect(firstTarget);
    assert.match(
      await page.evaluate("document.body.innerText"),
      /refuses iframe/,
    );
    const denied = await page.evaluate(`Promise.race([
    (async () => {
      try { await window.__TAURI_INTERNALS__.invoke('sidecar_status'); return false; }
      catch { return true; }
    })(),
    new Promise(resolve => setTimeout(() => resolve(true), 1000))
  ])`);
    assert.equal(
      denied,
      true,
      "Remote tab must not access workspace credentials",
    );

    await invoke("browser_navigate", {
      tabId: first,
      url: `${origin}/redirect`,
    });
    await until(
      async () =>
        (await invoke("browser_snapshot")).tabs.find(
          (tab) =>
            tab.id === first &&
            tab.url === `${origin}/two` &&
            tab.canGoBack &&
            !tab.loading,
        ),
      "redirect URL and history",
    );
    await invoke("browser_action", { tabId: first, action: "back" });
    await until(
      async () =>
        (await invoke("browser_snapshot")).tabs.find(
          (tab) =>
            tab.id === first &&
            tab.url === `${origin}/one` &&
            tab.canGoForward &&
            !tab.loading,
        ),
      "back",
    );
    await invoke("browser_action", { tabId: first, action: "forward" });
    await until(
      async () =>
        (await invoke("browser_snapshot")).tabs.find(
          (tab) =>
            tab.id === first && tab.url === `${origin}/two` && !tab.loading,
        ),
      "forward",
    );
    await invoke("browser_action", { tabId: first, action: "reload" });
    await until(
      async () =>
        (await invoke("browser_snapshot")).tabs.find(
          (tab) => tab.id === first && !tab.loading,
        ),
      "reload",
    );

    state = await invoke("browser_create_tab", { url: `${origin}/other` });
    const second = state.activeTabId;
    created.add(second);
    await until(
      async () =>
        (await invoke("browser_snapshot")).tabs.find(
          (tab) =>
            tab.id === second &&
            !tab.loading &&
            tab.title === "Browser fixture /other",
        ),
      "second independent tab",
    );
    state = await invoke("browser_activate_tab", { tabId: first });
    assert.equal(state.activeTabId, first);
    assert.equal(
      state.tabs.find((tab) => tab.id === first).url,
      `${origin}/two`,
    );
    await invoke("browser_set_layout", { bounds: null });
    assert.equal(
      (await invoke("browser_snapshot")).tabs.length,
      before.tabs.length + 2,
      "Hiding must preserve tabs",
    );
    await invoke("browser_set_layout", {
      bounds: { x: 400, y: 180, width: 420, height: 340 },
    });
    const popupRect = await page.evaluate(
      `(() => { const r = document.getElementById('popup').getBoundingClientRect(); return {x:r.x+r.width/2,y:r.y+r.height/2}; })()`,
    );
    await page.send("Input.dispatchMouseEvent", {
      type: "mousePressed",
      ...popupRect,
      button: "left",
      clickCount: 1,
    });
    await page.send("Input.dispatchMouseEvent", {
      type: "mouseReleased",
      ...popupRect,
      button: "left",
      clickCount: 1,
    });
    const popup = await until(
      async () =>
        (await invoke("browser_snapshot")).tabs.find(
          (tab) => tab.url === `${origin}/popup`,
        ),
      "target=_blank opens managed tab",
    );
    created.add(popup.id);

    for (const url of [
      "file:///C:/Windows/win.ini",
      "javascript:alert(1)",
      "https://name:secret@example.com",
    ]) {
      let rejected = false;
      try {
        await invoke("browser_navigate", { tabId: first, url });
      } catch {
        rejected = true;
      }
      assert.equal(
        rejected,
        true,
        `Must reject privileged/credential URL: ${url.split(":")[0]}`,
      );
    }
    state = await invoke("browser_close_tab", { tabId: second });
    created.delete(second);
    assert.equal(
      state.tabs.some((tab) => tab.id === second),
      false,
    );
    console.log(
      "PASS: native rendering with frame denial, IPC isolation, navigation/redirect/title/history, reload, independent tabs, hide/restore, popup tabs, URL validation and close.",
    );
  }
} finally {
  if (main) {
    // Include a popup even if a later assertion failed before it was recorded.
    const state = await invoke("browser_snapshot").catch(() => null);
    for (const tab of state?.tabs ?? []) {
      if (tab.url.startsWith(`http://127.0.0.1:${fixture.address()?.port}/`))
        created.add(tab.id);
    }
    for (const tabId of created)
      await invoke("browser_close_tab", { tabId }).catch(() => {});
    if (originalActive)
      await invoke("browser_activate_tab", { tabId: originalActive }).catch(
        () => {},
      );
    await invoke("browser_set_layout", { bounds: null }).catch(() => {});
  }
  for (const client of clients) client.close();
  fixture.closeAllConnections();
  await new Promise((resolve) => fixture.close(resolve));
}
