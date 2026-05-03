// httpssh -- minimal browser test client.
// Vanilla JS using xterm.js loaded from a CDN. No build step.
//
// Storage keys (localStorage):
//   httpssh.lanBearer        - LAN bearer string
//   httpssh.cfClientId       - Cloudflare Service Token Client ID (dev-only)
//   httpssh.cfClientSecret   - Cloudflare Service Token Client Secret (dev-only)

(function () {
  "use strict";

  const LS = window.localStorage;

  const state = {
    sessions: [],
    activeId: null,
    tabs: new Map(),
  };

  // Polling timer for /api/sessions; cleared when the Cloudflare Access
  // session expires so we stop hitting the edge with doomed requests.
  let refreshTimer = null;

  document.getElementById("origin").textContent = location.origin;

  // ----- auth headers -----

  function authHeaders() {
    const h = {};
    const bearer = LS.getItem("httpssh.lanBearer") || "";
    if (bearer) h["Authorization"] = "Bearer " + bearer;
    const cfId = LS.getItem("httpssh.cfClientId") || "";
    const cfSecret = LS.getItem("httpssh.cfClientSecret") || "";
    if (cfId && cfSecret) {
      h["CF-Access-Client-Id"] = cfId;
      h["CF-Access-Client-Secret"] = cfSecret;
    }
    return h;
  }

  // Tracks whether the Cloudflare Access cookie has expired in mid-session.
  // When set, the 5-second polling halts and the SPA shows a sticky
  // "Sign in again" banner instead of repeatedly bouncing off
  // /cdn-cgi/access/login (which produces "Failed to fetch" noise in
  // the console and a flood of failed CORS preflights against
  // <team>.cloudflareaccess.com).
  let cfSessionExpired = false;

  function markCfSessionExpired() {
    if (cfSessionExpired) return;
    cfSessionExpired = true;
    if (refreshTimer) clearInterval(refreshTimer);
    refreshTimer = null;
    showReauthOverlay();
  }

  function showReauthOverlay() {
    let el = document.getElementById("reauth-overlay");
    if (!el) {
      el = document.createElement("div");
      el.id = "reauth-overlay";
      el.innerHTML =
        "<div><h2>Cloudflare Access session expired</h2>" +
        "<p>The browser session cookie set by your Google login is no longer valid. Reload the page to sign in again.</p>" +
        "<button id=\"reauth-reload\">Reload</button></div>";
      document.body.appendChild(el);
      el.querySelector("#reauth-reload").addEventListener("click", () => {
        // Full-page navigation, not fetch — Cloudflare Access intercepts
        // and drives the Google SSO flow from scratch.
        window.location.reload();
      });
    }
    el.classList.add("visible");
  }

  // Wrap fetch so that:
  //   - cookies (CF_Authorization) always travel with the request,
  //   - 3xx redirects from Cloudflare Access surface as an
  //     opaqueredirect response which we promote to a clear "session
  //     expired" UI instead of a confusing "Failed to fetch" after the
  //     browser tries to follow them cross-origin,
  //   - one transient TypeError: Failed to fetch (Cloudflare connector
  //     blip, mid-request 302, etc.) is auto-retried once before
  //     surfacing to the user.
  async function api(path, opts) {
    if (cfSessionExpired) {
      throw new Error("Cloudflare Access session expired");
    }
    const o = Object.assign({}, opts || {});
    o.headers = Object.assign({ "Content-Type": "application/json" }, authHeaders(), o.headers || {});
    o.credentials = "include";
    o.redirect = "manual"; // we want to see Cloudflare's 302 ourselves

    let lastErr;
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        const res = await fetch(path, o);
        // With redirect: 'manual', a 3xx from Cloudflare Access becomes
        // an opaque-redirect response with status 0 and type === 'opaqueredirect'.
        if (res.type === "opaqueredirect" || res.status === 0) {
          markCfSessionExpired();
          throw new Error("Cloudflare Access session expired");
        }
        if (!res.ok) {
          let msg = res.status + " " + res.statusText;
          try {
            const body = await res.json();
            if (body && body.error) msg = body.error.code + ": " + body.error.message;
          } catch (_) {}
          throw new Error(msg);
        }
        if (res.status === 204) return null;
        return res.json();
      } catch (e) {
        lastErr = e;
        const msg = (e && e.message) || String(e);
        const isTransient = msg === "Failed to fetch" || msg.startsWith("network");
        if (!isTransient || attempt > 0) break;
        // Brief backoff before the retry; some Cloudflare Tunnel blips
        // resolve in well under a second.
        await new Promise((r) => setTimeout(r, 400));
      }
    }
    throw lastErr;
  }

  // ----- session list -----

  async function refresh() {
    try {
      const list = await api("/api/sessions");
      state.sessions = list.sessions || [];
      renderSessionList();
    } catch (e) {
      showBanner("Failed to list sessions: " + e.message);
    }
  }

  function renderSessionList() {
    const ul = document.getElementById("session-list");
    ul.innerHTML = "";
    state.sessions.forEach((s) => {
      const li = document.createElement("li");
      li.className = state.activeId === s.id ? "active" : "";
      li.innerHTML =
        "<strong></strong><br><span class=\"meta\"></span>" +
        "<span class=\"kill\" title=\"Kill session\">x</span>";
      li.querySelector("strong").textContent = s.title;
      li.querySelector(".meta").textContent =
        s.shell.split(/[\\/]/).pop() + " - " + s.cols + "x" + s.rows + " - " + s.subscribers + " sub";
      li.addEventListener("click", (ev) => {
        if (ev.target.classList.contains("kill")) return;
        attachOrFocus(s.id, s.title);
      });
      li.querySelector(".kill").addEventListener("click", async () => {
        if (!confirm("Kill session " + s.title + "?")) return;
        try {
          await api("/api/sessions/" + s.id, { method: "DELETE" });
          closeTab(s.id);
          refresh();
        } catch (e) {
          alert("Kill failed: " + e.message);
        }
      });
      ul.appendChild(li);
    });
  }

  // ----- tab + terminal management -----

  function makeTab(id, title) {
    const term = new Terminal({
      convertEol: false,
      cursorBlink: true,
      theme: { background: "#000000" },
      fontFamily: "Consolas, 'Cascadia Mono', 'Courier New', monospace",
      fontSize: 14,
    });
    const fit = new FitAddon.FitAddon();
    term.loadAddon(fit);

    const host = document.createElement("div");
    host.className = "xterm-host";
    document.getElementById("terminal-container").appendChild(host);
    term.open(host);

    const tab = {
      id, title, term, fit, host,
      ws: null, retryAttempt: 0, retryTimer: null, closing: false,
      wsGen: 0,
      el: null, dot: null,
    };

    const tabEl = document.createElement("div");
    tabEl.className = "tab";
    tabEl.innerHTML = "<span class=\"dot\"></span><span class=\"name\"></span><span class=\"close\">x</span>";
    tabEl.querySelector(".name").textContent = title;
    tabEl.addEventListener("click", (ev) => {
      if (ev.target.classList.contains("close")) return;
      focusTab(id);
    });
    tabEl.querySelector(".close").addEventListener("click", () => closeTab(id));
    document.getElementById("tabs").appendChild(tabEl);
    tab.el = tabEl;
    tab.dot = tabEl.querySelector(".dot");

    term.onData((data) => {
      if (tab.ws && tab.ws.readyState === WebSocket.OPEN) {
        tab.ws.send(JSON.stringify({ t: "in", d: data }));
      }
    });
    window.addEventListener("resize", () => safeFit(tab));

    state.tabs.set(id, tab);
    connect(tab);
    return tab;
  }

  function safeFit(tab) {
    try {
      tab.fit.fit();
      const dims = { c: tab.term.cols, r: tab.term.rows };
      if (tab.ws && tab.ws.readyState === WebSocket.OPEN) {
        tab.ws.send(JSON.stringify({ t: "resize", c: dims.c, r: dims.r }));
      }
    } catch (_) {}
  }

  function connect(tab) {
    setDot(tab, "reconnecting");
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const params = new URLSearchParams();
    const bearer = LS.getItem("httpssh.lanBearer") || "";
    if (bearer) params.set("token", bearer);
    const url = proto + "//" + location.host + "/api/sessions/" + tab.id + "/io" + (params.toString() ? "?" + params : "");

    const ws = new WebSocket(url, ["httpssh.v1"]);
    const myGen = ++tab.wsGen;
    tab.ws = ws;
    tab.closing = false;

    ws.addEventListener("open", () => {
      if (myGen !== tab.wsGen) return;
      tab.retryAttempt = 0;
      setDot(tab, "ok");
      hideBanner();
      requestAnimationFrame(() => safeFit(tab));
    });

    ws.addEventListener("message", (ev) => {
      if (myGen !== tab.wsGen) return;
      let frame;
      try { frame = JSON.parse(ev.data); } catch (_) { return; }
      switch (frame.t) {
        case "replay":
          tab.term.reset();
          tab.term.clear();
          tab.term.write(frame.d || "");
          break;
        case "out":
          tab.term.write(frame.d || "");
          break;
        case "exit":
          tab.term.write("\r\n[process exited code=" + (frame.code != null ? frame.code : "?") + "]\r\n");
          setDot(tab, "closed");
          tab.closing = true;
          break;
        case "pong":
          break;
        case "error":
          showBanner("Server: " + (frame.message || "(no message)"));
          break;
      }
    });

    ws.addEventListener("close", () => {
      if (myGen !== tab.wsGen) return;
      tab.ws = null;
      if (tab.closing) {
        setDot(tab, "closed");
        return;
      }
      setDot(tab, "reconnecting");
      const delays = [1000, 2000, 5000, 10000, 30000];
      const wait = delays[Math.min(tab.retryAttempt, delays.length - 1)];
      tab.retryAttempt++;
      showBanner("Reconnecting in " + Math.round(wait / 1000) + "s...");
      tab.retryTimer = setTimeout(() => {
        if (!tab.closing) connect(tab);
      }, wait);
    });

    const pingInterval = setInterval(() => {
      if (myGen !== tab.wsGen) {
        clearInterval(pingInterval);
        try { ws.close(); } catch (_) {}
        return;
      }
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t: "ping" }));
      else clearInterval(pingInterval);
    }, 20000);
  }

  function setDot(tab, state) {
    if (!tab.dot) return;
    tab.dot.className = "dot" + (state === "reconnecting" ? " reconnecting" : state === "closed" ? " closed" : "");
  }

  function focusTab(id) {
    state.activeId = id;
    state.tabs.forEach((tab, tabId) => {
      tab.host.classList.toggle("active", tabId === id);
      if (tab.el) tab.el.classList.toggle("active", tabId === id);
    });
    const tab = state.tabs.get(id);
    if (tab) requestAnimationFrame(() => safeFit(tab));
    renderSessionList();
  }

  async function attachOrFocus(id, title) {
    if (state.tabs.has(id)) {
      focusTab(id);
      return;
    }
    makeTab(id, title || id.slice(0, 6));
    focusTab(id);
  }

  function closeTab(id) {
    const tab = state.tabs.get(id);
    if (!tab) return;
    tab.closing = true;
    tab.wsGen++;
    if (tab.retryTimer) clearTimeout(tab.retryTimer);
    if (tab.ws) tab.ws.close();
    if (tab.el) tab.el.remove();
    if (tab.host) tab.host.remove();
    state.tabs.delete(id);
    if (state.activeId === id) {
      const next = state.tabs.keys().next().value || null;
      if (next) focusTab(next);
      else state.activeId = null;
    }
  }

  // ----- ui -----

  document.getElementById("new-btn").addEventListener("click", async () => {
    try {
      const info = await api("/api/sessions", { method: "POST", body: JSON.stringify({ shell: "pwsh" }) });
      await refresh();
      attachOrFocus(info.id, info.title);
    } catch (e) {
      alert("Create failed: " + e.message);
    }
  });

  const dlg = document.getElementById("settings");
  document.getElementById("settings-btn").addEventListener("click", () => {
    document.getElementById("lan-bearer").value = LS.getItem("httpssh.lanBearer") || "";
    document.getElementById("cf-id").value = LS.getItem("httpssh.cfClientId") || "";
    document.getElementById("cf-secret").value = LS.getItem("httpssh.cfClientSecret") || "";
    dlg.showModal();
  });
  document.getElementById("save-settings").addEventListener("click", () => {
    LS.setItem("httpssh.lanBearer", document.getElementById("lan-bearer").value);
    LS.setItem("httpssh.cfClientId", document.getElementById("cf-id").value);
    LS.setItem("httpssh.cfClientSecret", document.getElementById("cf-secret").value);
    dlg.close();
    refresh();
  });
  document.getElementById("close-settings").addEventListener("click", () => dlg.close());

  function showBanner(msg) {
    const b = document.getElementById("status-banner");
    b.textContent = msg;
    b.hidden = false;
  }
  function hideBanner() {
    document.getElementById("status-banner").hidden = true;
  }

  refresh();
  refreshTimer = setInterval(refresh, 5000);
})();
