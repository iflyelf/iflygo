# Vendored Third-Party Scripts

## webrtc-adapter

- File: `adapter-9.0.1.min.js`
- Version: `9.0.1`
- Source: `https://cdn.jsdelivr.net/npm/webrtc-adapter@9.0.1/out/adapter.min.js`
- Notes:
  - This file is pinned to a fixed version for deterministic behavior.
  - `index.html` loads this local file first, then falls back to the same pinned CDN URL if local load fails.
