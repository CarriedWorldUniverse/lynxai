# Operations

This page covers self-hosting lynxai: configuration, filesystem layout, security posture, backup, LLM and browser setup, logs, signals, and the known limitations in v1.

## Configuration

lynxai is configured via flags to `lynxai serve` and a small set of env vars. There is no config file.

### Flags

| Flag              | Default                | Description                                                              |
|-------------------|------------------------|--------------------------------------------------------------------------|
| `--addr`          | `127.0.0.1:7878`       | Bind address.                                                            |
| `--data-dir`      | `$LYNXAI_DATA_DIR` or `~/.lynxai` | Directory holding `master.key` and `lynxai.db`.              |
| `--bridle-config` | `$LYNXAI_BRIDLE_CONFIG` (empty) | Reserved. **File-based config loading is deferred in v1** — see [LLM provider](#llm-provider). |

### Environment variables

| Variable                  | Purpose                                                                                       |
|---------------------------|-----------------------------------------------------------------------------------------------|
| `LYNXAI_DATA_DIR`         | Default for `--data-dir`.                                                                     |
| `LYNXAI_BRIDLE_CONFIG`    | Default for `--bridle-config`. Setting this in v1 causes lynxai to fail to start (see below). |
| `LYNXAI_LLM_API_KEY`      | API key for the default DeepSeek (`openai-api`) provider. Required unless `--bridle-config` is set (and v1 errors on that anyway, so: required, full stop). |

## Filesystem layout

Under `<data-dir>` (default `~/.lynxai`):

| File         | Contents                                                  | Perms     |
|--------------|-----------------------------------------------------------|-----------|
| `master.key` | 32 bytes of AES key material (random on first start).     | `0600`    |
| `lynxai.db`  | SQLite — encrypted credential bundles + audit log.        | OS default (typically `0644`) |

`<data-dir>` itself is created with mode `0700`.

The Docker image sets `LYNXAI_DATA_DIR=/data` and declares `VOLUME /data` — mount a named volume or host path to persist state across container restarts.

## Security stance

### No built-in API auth

lynxai has **no authentication** on its HTTP endpoints. There is no API key, no JWT, no Basic auth. The defense is twofold:

1. The default bind address is `127.0.0.1:7878` — loopback only.
2. If you need network exposure, put lynxai behind a reverse proxy that enforces auth.

Example nginx config with HTTP Basic in front of lynxai:

```nginx
server {
    listen 443 ssl http2;
    server_name lynxai.internal;

    ssl_certificate     /etc/ssl/lynxai.crt;
    ssl_certificate_key /etc/ssl/lynxai.key;

    location / {
        auth_basic           "lynxai";
        auth_basic_user_file /etc/nginx/.htpasswd-lynxai;

        proxy_pass         http://127.0.0.1:7878;
        proxy_set_header   Host $host;
        proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 120s;
    }
}
```

Generate the password file with `htpasswd -c /etc/nginx/.htpasswd-lynxai agentname`. Anyone with the proxy's password can read/write every credential in the vault — treat it accordingly.

### Master key handling

!!! warning "Back up master.key alongside the DB"
    `master.key` is the only key for `lynxai.db`. Lose it and every stored credential becomes ciphertext you can't open. AES-256-GCM is authenticated; there is no rescue path.

!!! warning "Don't relax permissions"
    `<data-dir>` is `0700`, `master.key` is `0600`. Don't `chmod` either to something more permissive. Don't commit `master.key` to git. Don't sync the data dir through cloud storage that doesn't preserve permissions.

### Audit log retention

There is no rotation or retention policy in v1. The audit log grows forever. If it gets large, prune it manually:

```bash
sqlite3 ~/.lynxai/lynxai.db "DELETE FROM credential_audit WHERE used_at < strftime('%s', 'now', '-90 days');"
```

Audit rows for deleted credentials are not removed when the credential is deleted.

## Backup strategy

You must back up **both** files together:

- `<data-dir>/master.key`
- `<data-dir>/lynxai.db`

One without the other is useless. Either is enough to fully restore the vault if you have both.

### SQLite consistency

lynxai opens SQLite with the default journal mode (no WAL). For a consistent snapshot you have two options:

**Shut down lynxai, copy, restart:**

```bash
docker stop lynxai
cp ~/.lynxai/master.key   /backup/master.key
cp ~/.lynxai/lynxai.db    /backup/lynxai.db
docker start lynxai
```

**Hot backup with `sqlite3 .dump`:**

```bash
sqlite3 ~/.lynxai/lynxai.db ".backup '/backup/lynxai.db'"
cp ~/.lynxai/master.key /backup/master.key
```

`.backup` uses SQLite's online backup API and is safe while lynxai is running.

Store backups encrypted at rest — `master.key` is exactly as sensitive as the credentials it protects.

## LLM provider

lynxai uses [bridle](https://github.com/CarriedWorldUniverse/bridle) as the LLM harness for `/extract`.

### v1 supported configuration

- **Provider:** `openai-api` (any OpenAI-API-compatible endpoint).
- **Base URL:** hardcoded to `https://api.deepseek.com`.
- **Model:** hardcoded to `deepseek-chat`.
- **API key:** from `LYNXAI_LLM_API_KEY`.

This is the only path in v1. If `LYNXAI_LLM_API_KEY` is unset and `--bridle-config` is empty, `lynxai serve` exits with:

```
bridle config: no bridle config and LYNXAI_LLM_API_KEY not set (need either)
```

### `--bridle-config` is deferred

The `--bridle-config` flag and `LYNXAI_BRIDLE_CONFIG` env var exist in v1, but file-based config loading is **not implemented**. Setting either causes startup to fail with:

```
bridle config: --bridle-config / LYNXAI_BRIDLE_CONFIG is not supported in v1 (got path "..."). Omit the flag and set LYNXAI_LLM_API_KEY for the default DeepSeek config; file-based bridle config is planned for a later release.
```

### Using a non-DeepSeek provider in v1

If you need a different base URL (e.g. you want to point at OpenAI directly, Anthropic via a proxy, or a local Ollama), v1 doesn't give you a config knob. Options:

- Wait for the file-based bridle config loader in a later release.
- Fork lynxai and change the constants in `internal/bridlecfg/config.go`.
- Run an OpenAI-compatible proxy (e.g. LiteLLM) on `https://api.deepseek.com` from lynxai's perspective.

## Chromium / browser

lynxai uses [chromedp](https://github.com/chromedp/chromedp) to drive a headless browser.

### Docker

The image is built on `chromedp/headless-shell:latest`, which ships a Chromium-equivalent binary on `PATH`. Nothing to install.

### Binary install

chromedp auto-discovers Chromium via its `DefaultExecAllocatorOptions`. It looks for, in order: `headless-shell`, `chromium-browser`, `chromium`, `chrome`, `google-chrome`, `google-chrome-stable`, and (on macOS) `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`. Have one of these on `PATH` (or the macOS path) and it'll be found.

Common installation paths:

- **macOS:** Install Google Chrome from google.com/chrome.
- **Debian/Ubuntu:** `apt install chromium` or `apt install chromium-browser`.
- **Arch:** `pacman -S chromium`.
- **Fedora:** `dnf install chromium`.

### Memory and process model

- **One allocator process.** lynxai starts a single chromedp `ExecAllocator` at boot and reuses it for every fetch. The underlying Chromium binary stays running between requests.
- **Fresh context per fetch.** Each `/fetch` (and the fetch inside `/extract`) gets a fresh `chromedp.NewContext` — its own browser context, no cookie or storage bleed between requests except what credentials explicitly inject.
- **Per-request timeout:** 30 seconds by default. Hardcoded in v1.
- **No browser pool.** `engine.Config.PoolSize` exists in the source as reserved for v1.1 but is unused.

Headless Chromium running in a container typically wants 200–500MB resident; budget accordingly. The default flags include `--no-sandbox` (necessary inside containers) and `--disable-gpu`.

## Logs

lynxai logs to stdout via the Go standard `log` package. Two lines are guaranteed:

```
lynxai serving on http://127.0.0.1:7878 (data-dir=/home/you/.lynxai, llm=openai-api/deepseek-chat)
shutting down...
```

`ListenAndServe` errors (other than the graceful `ErrServerClosed`) and audit-write failures are logged but otherwise non-fatal. There are no structured logs, no log levels, no `--log-format json` in v1.

## Signals

`SIGINT` and `SIGTERM` trigger graceful shutdown:

1. `srv.Shutdown` is called with a 10-second grace.
2. In-flight requests finish (or are cancelled at 10s).
3. The chromedp allocator is closed.
4. The vault DB is closed.

Docker's default `docker stop` sends SIGTERM, waits 10s, then SIGKILL — which lines up exactly with lynxai's grace period.

## Known limitations in v1

The honest list. None of these are deal-breakers for the intended use case (an AI agent fetching and extracting from a small set of known sites), but you should know them before you ship.

- **`FetchResult.Status` is always `200`** on successful navigation. The real HTTP status isn't surfaced. Planned for v1.1 via a CDP network listener.
- **LLM provider errors come back as `extraction_failed` (502),** not `llm_unavailable` (503). The `llm_unavailable` code is reserved but not currently emitted.
- **No API authentication.** Bind loopback or front with a reverse proxy.
- **No metrics or observability endpoints.** No `/metrics`, no Prometheus, no OpenTelemetry. Stdout logs are it.
- **SQLite is single-process.** Don't run two `lynxai serve` instances against the same `--data-dir` — without WAL, you'll corrupt the DB.
- **Form-login sessions don't persist across restarts.** The cache is in-memory only; restart triggers re-login on next use.
- **No OAuth credential kind.** OAuth tokens have to be re-stored as `bearer` when they rotate.
- **No host-matching enforcement.** lynxai will apply any credential to any URL you ask it to. Be careful.
- **No credential rotation API.** Overwrite by re-PUTting the same `name`. No rotate-token endpoint.
- **No master-key rotation tooling.** You can't re-key the vault in place. Pick a host you trust to keep `master.key` for the long haul.
- **No `--bridle-config` file loading.** v1 only supports the DeepSeek default via `LYNXAI_LLM_API_KEY`. Forking is the workaround for other providers.
- **No retry-on-401 invalidation.** Stale form-login cookies stay cached until process restart.
- **Audit log retention is forever** (no rotation in v1). Prune manually if needed.
