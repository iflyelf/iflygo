(function (root, factory) {
  const api = factory();

  if (typeof module === "object" && module.exports) {
    module.exports = api;
  }
  if (root) {
    root.CrossDeskTurnCredentials = api;
  }
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  function normalizeHost(value) {
    if (typeof value !== "string") return null;

    let host = value.trim();
    if (!host || /\s/.test(host) || /[/?#@]/.test(host)) return null;

    if (host.startsWith("[") || host.endsWith("]")) {
      if (!host.startsWith("[") || !host.endsWith("]")) return null;
      host = host.slice(1, -1);
      if (!host || host.includes("[") || host.includes("]")) return null;
    }

    return host;
  }

  function formatHostForUrl(host) {
    return host.includes(":") ? `[${host}]` : host;
  }

  function parseTurnCredentials(turn, nowSeconds = Math.floor(Date.now() / 1000)) {
    if (!turn || typeof turn !== "object" || Array.isArray(turn)) return null;

    const host = normalizeHost(turn.host);
    const port = turn.port;
    const username = typeof turn.username === "string" ? turn.username : "";
    const password = typeof turn.password === "string" ? turn.password : "";
    const expiresAt = turn.expires_at;

    if (
      !host ||
      !Number.isInteger(port) ||
      port < 1 ||
      port > 65535 ||
      !username ||
      !password ||
      !Number.isSafeInteger(expiresAt) ||
      expiresAt <= nowSeconds
    ) {
      return null;
    }

    const urlHost = formatHostForUrl(host);
    return {
      host,
      port,
      expiresAt,
      iceServer: {
        urls: [
          `turn:${urlHost}:${port}?transport=udp`,
          `turn:${urlHost}:${port}?transport=tcp`,
        ],
        username,
        credential: password,
      },
    };
  }

  // Match the signaling service's version-1 per-connection ICE configuration.
  // Validate atomically: a malformed list must never partially replace a list
  // or mix credentials from different connection attempts.
  function parseIceConfiguration(ice, nowSeconds = Math.floor(Date.now() / 1000)) {
    if (!ice || typeof ice !== "object" || Array.isArray(ice) || ice.version !== 1 ||
        typeof ice.id !== "string" || !/^[0-9a-fA-F-]{1,64}$/.test(ice.id) ||
        !Number.isSafeInteger(ice.expires_at) || ice.expires_at <= nowSeconds + 5 ||
        !Array.isArray(ice.servers) || ice.servers.length > 16) return null;
    const servers = new Map();
    let urlCount = 0, stunCount = 0, turnCount = 0;
    let expiresAt = ice.expires_at;
    for (const entry of ice.servers) {
      if (!entry || typeof entry !== "object" || Array.isArray(entry)) return null;
      const urls = typeof entry.urls === "string" ? [entry.urls] : entry.urls;
      if (!Array.isArray(urls) || !urls.length) return null;
      for (const url of urls) {
        if (++urlCount > 32 || typeof url !== "string" || url.length > 512) return null;
        const match = /^(stun|turn):(\[[0-9A-Za-z:.%_-]+\]|[A-Za-z0-9._-]+)(?::([0-9]{1,5}))?(?:\?transport=(udp|tcp))?$/.exec(url);
        if (!match) return null;
        const [, scheme, host, portText, transport] = match;
        const port = portText === undefined ? 3478 : Number(portText);
        if (port < 1 || port > 65535 || host.length + 1 + String(port).length > 255 ||
            (host.startsWith("[") && !host.includes(":")) || (scheme === "stun" && transport)) return null;
        const normalizedHost = host.replace(/^[^%]+/, value => value.toLowerCase());
        const canonical = `${scheme}:${normalizedHost}:${port}` +
          (scheme === "turn" ? `?transport=${transport || "udp"}` : "");
        const server = { urls: [canonical] };
        if (scheme === "turn") {
          if (![entry.username, entry.credential].every(value =>
              typeof value === "string" && value.length > 0 && value.length <= 512 && !value.includes("\0")) ||
              !Number.isSafeInteger(entry.expires_at) || entry.expires_at <= nowSeconds + 5) return null;
          server.username = entry.username;
          server.credential = entry.credential;
          expiresAt = Math.min(expiresAt, entry.expires_at);
        }
        const previous = servers.get(canonical);
        if (previous) {
          if (previous.server.username !== server.username || previous.server.credential !== server.credential ||
              (scheme === "turn" && previous.expiresAt !== entry.expires_at)) return null;
          continue;
        }
        if (scheme === "turn" ? ++turnCount > 8 : ++stunCount > 8) return null;
        servers.set(canonical, { server, expiresAt: entry.expires_at });
      }
    }
    return { id: ice.id, expiresAt, iceServers: Array.from(servers.values(), entry => entry.server) };
  }

  return { parseTurnCredentials, parseIceConfiguration };
});
