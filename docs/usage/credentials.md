# Credentials

lynxai stores credentials encrypted at rest and applies them at fetch time. Clients reference credentials by name only — bundle contents never come back out over the API.

This page covers the four credential kinds, exact bundle shapes, how each is applied, and how the audit log works.

## The four kinds

| Kind      | Use when…                                                                                       |
|-----------|-------------------------------------------------------------------------------------------------|
| `basic`   | The target uses HTTP Basic auth.                                                                |
| `bearer`  | The target accepts a static bearer/API token in an `Authorization` header.                      |
| `cookies` | You have pre-baked session cookies (e.g. exported from your browser).                           |
| `form`    | The target uses a standard HTML login form — lynxai POSTs once and reuses the session cookie.   |

All four are stored as JSON bundles, encrypted with AES-256-GCM (per-row nonce; key derived via HKDF-SHA256 from `master.key`). The vault schema and audit log live in the same SQLite DB.

## Scoping (the `host` field)

Every credential has a `host` field at the top level of the `POST /credentials` request body **and** inside the bundle itself. This is the host the credential is intended for — it's metadata (so you can list credentials by host, audit by host, etc.) and is used by validators. lynxai does **not** currently enforce that the URL you `/fetch` matches the credential's host: you can use a credential against any URL and it'll be applied. Be careful not to send a credential to the wrong place.

---

## `basic`

HTTP Basic auth. lynxai builds `Authorization: Basic base64(user:password)` and sends it as an extra header before navigation (via CDP `Network.setExtraHTTPHeaders`).

### Bundle shape

```json
{
  "host": "example.com",
  "user": "alice",
  "password": "hunter2"
}
```

All three fields required.

### Full example

```bash
curl -sS -X POST http://localhost:7878/credentials \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "wiki-staging",
    "kind": "basic",
    "host": "staging.wiki.example.com",
    "bundle": {
      "host": "staging.wiki.example.com",
      "user": "alice",
      "password": "hunter2"
    }
  }'
```

---

## `bearer`

A static bearer token (API key, personal access token, etc.). lynxai sends `Authorization: Bearer <token>` as an extra header.

### Bundle shape

```json
{
  "host": "api.github.com",
  "token": "ghp_xxxxxxxxxxxx"
}
```

Both fields required.

### Full example

```bash
curl -sS -X POST http://localhost:7878/credentials \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "github-mine",
    "kind": "bearer",
    "host": "api.github.com",
    "bundle": {
      "host": "api.github.com",
      "token": "ghp_xxxxxxxxxxxx"
    }
  }'
```

---

## `cookies`

A pre-baked cookie jar applied via CDP `Network.setCookies` before navigation. Use this when you have a session cookie exported from a real browser session, or when the target only uses cookie auth (no Authorization header).

### Bundle shape

```json
{
  "host": "app.example.com",
  "cookies": [
    {
      "name": "session_id",
      "value": "abc123",
      "domain": ".example.com",
      "path": "/",
      "secure": true,
      "http_only": true
    }
  ]
}
```

| Field              | Type    | Required | Description                              |
|--------------------|---------|----------|------------------------------------------|
| `host`             | string  | yes      | Logical host the bundle is for.          |
| `cookies`          | array   | yes      | At least one cookie required.            |
| `cookies[].name`   | string  | yes      |                                          |
| `cookies[].value`  | string  | yes      |                                          |
| `cookies[].domain` | string  | no       | Defaults to cookie's natural scope.      |
| `cookies[].path`   | string  | no       |                                          |
| `cookies[].secure` | bool    | no       |                                          |
| `cookies[].http_only` | bool | no       |                                          |

### Full example

```bash
curl -sS -X POST http://localhost:7878/credentials \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "wiki-session",
    "kind": "cookies",
    "host": "wiki.example.com",
    "bundle": {
      "host": "wiki.example.com",
      "cookies": [
        {"name":"wiki_session","value":"abc123","domain":".example.com","path":"/","secure":true,"http_only":true}
      ]
    }
  }'
```

---

## `form`

The interesting one. Use when the target is a standard HTML login form (`POST /login` with `application/x-www-form-urlencoded` body).

lynxai performs the form-login POST itself (not via Chromium) using Go's `net/http`, captures cookies via a `cookiejar`, verifies the configured success cookie is present, and applies the resulting cookies to the browser context before navigation. The cookies are cached in memory keyed by credential name — subsequent fetches reuse them until the process restarts.

### Bundle shape

```json
{
  "host": "app.example.com",
  "login_url": "https://app.example.com/login",
  "method": "POST",
  "fields": {
    "user_field": "username",
    "pass_field": "password",
    "user": "alice",
    "password": "hunter2"
  },
  "success_cookie_name": "session_id"
}
```

| Field                              | Type   | Required | Description                                                            |
|------------------------------------|--------|----------|------------------------------------------------------------------------|
| `host`                             | string | yes      | Logical host.                                                          |
| `login_url`                        | string | yes      | Absolute URL of the login form's POST target.                          |
| `method`                           | string | no       | Defaults to `POST`. Only `POST` is supported in v1.                    |
| `fields.user_field`                | string | yes      | The form field name for the username (e.g. `email`, `username`).       |
| `fields.pass_field`                | string | yes      | The form field name for the password.                                  |
| `fields.user`                      | string | no       | The username value to submit. (Validator allows empty; provide it.)    |
| `fields.password`                  | string | no       | The password value to submit. (Validator allows empty; provide it.)    |
| `success_cookie_name`              | string | yes      | Cookie name that proves login worked. If absent from the response, `credential_apply_failed`. |

### How application works

1. First `/fetch` or `/extract` that references a `form` credential triggers a login.
2. lynxai POSTs `user_field=<user>&pass_field=<password>` (URL-encoded) to `login_url`.
3. All cookies the server returns are captured into a per-credential jar.
4. If `success_cookie_name` is in the jar, the cookies are cached in memory keyed by credential name; otherwise `credential_apply_failed` fires.
5. The cookies are applied to the browser context via CDP `Network.setCookies` before navigation.
6. Subsequent fetches with the same credential reuse the cached cookies — no re-login.

Concurrent first-logins for the same credential are deduplicated via singleflight: only one POST happens; everyone waits.

### Full example

```bash
curl -sS -X POST http://localhost:7878/credentials \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "dashboard-alice",
    "kind": "form",
    "host": "dashboard.example.com",
    "bundle": {
      "host": "dashboard.example.com",
      "login_url": "https://dashboard.example.com/login",
      "method": "POST",
      "fields": {
        "user_field": "email",
        "pass_field": "password",
        "user": "alice@example.com",
        "password": "hunter2"
      },
      "success_cookie_name": "session_id"
    }
  }'
```

### Form-login limitations in v1

- **Sessions don't persist across restarts.** The cache is in-memory only. Restart lynxai and the next `/fetch` triggers a fresh login.
- **No invalidation API.** If a session goes stale mid-process you'll get errors on `/fetch` until lynxai restarts. (There's an internal `Invalidate` method but it's not wired to an HTTP endpoint in v1.)
- **POST only.** `method` defaults to `POST`; nothing else is meaningfully supported.
- **No JavaScript / CSRF token handling.** lynxai sends the configured fields as URL-encoded form data. If the login form needs a CSRF token, a JS-computed nonce, or multi-step flow, `form` won't work — use `cookies` with a session exported from a real browser.

---

## Audit log

Every credential use writes one row to the `credential_audit` table.

### What's recorded

| Column        | Description                                                          |
|---------------|----------------------------------------------------------------------|
| `name`        | Credential name (the same name used to reference it in the API).     |
| `used_at`     | Unix timestamp (seconds).                                            |
| `request_url` | The URL passed to `/fetch` or `/extract`.                            |
| `outcome`     | One of `ok`, `not_found`, `decrypt_failed`, `apply_failed`.          |

### What's **not** recorded

- The bundle contents. Tokens, passwords, cookie values never appear in the audit log.
- The response body or status code.
- Headers.

### Outcome semantics

- **`ok`** — the credential was successfully resolved and applied. **This does not mean the page fetch succeeded.** If navigation subsequently failed (timeout, DNS, etc.), the audit row is still `ok` because the credential itself worked. The API response separately surfaces the navigation error.
- **`not_found`** — the named credential doesn't exist in the vault. API returns `credential_not_found` (404).
- **`decrypt_failed`** — the vault row was found but AES-GCM open failed (wrong master key, corrupted ciphertext). API returns `credential_decrypt_failed` (500).
- **`apply_failed`** — currently only emitted for `form` credentials when the login POST fails or the success cookie isn't seen. **Navigation failures are not classified as `apply_failed`** — they're audited as `ok` (the credential was applied; the page just didn't load).

### Reading the audit log

v1 has no API endpoint for reading audit rows. Query the SQLite DB directly:

```bash
sqlite3 ~/.lynxai/lynxai.db \
  "SELECT name, datetime(used_at, 'unixepoch'), request_url, outcome FROM credential_audit ORDER BY used_at DESC LIMIT 20;"
```

Audit rows are kept forever in v1 — no rotation, no retention policy. If the table gets large, prune it yourself with `DELETE FROM credential_audit WHERE used_at < ...`.

Deleting a credential does **not** delete its audit rows.

---

## Security details

### Encryption at rest

- Algorithm: AES-256-GCM with a 12-byte random nonce per row.
- Key: 32 bytes derived from `master.key` via HKDF-SHA256 with info string `lynxai.credentials.v1`. Bumping the info string in a future release would force a full re-key.
- Master key: 32 random bytes generated by `crypto/rand` on first start, written atomically to `<data-dir>/master.key` with mode `0600`. lynxai uses `os.Link` for the install step so two concurrent first-starts can't silently produce two different keys.
- Per-row nonce + AES-GCM authentication tag are stored in the SQLite row alongside the ciphertext.

### File permissions

- `<data-dir>` is created with mode `0700`.
- `<data-dir>/master.key` is created with mode `0600`.
- `<data-dir>/lynxai.db` is whatever SQLite creates (typically `0644`); the contents are encrypted so this is acceptable, but the master key is the real secret — keep it locked down.

!!! warning "Back up master.key and lynxai.db together"
    Losing `master.key` makes `lynxai.db` unrecoverable. Backing up one without the other is useless. See [Operations → Backup strategy](operations.md#backup-strategy).

### What lynxai does not do (in v1)

- **No OAuth credential kind.** OAuth (with refresh tokens, PKCE, etc.) is on the roadmap; for now treat OAuth access tokens as `bearer` and re-store them when they expire.
- **No programmatic credential rotation.** Re-PUT the credential with the same `name` to overwrite. There's no `PATCH` or rotate-token endpoint.
- **No master-key rotation tooling.** You can't re-key the vault in place yet. Plan ahead: pick a host you trust to back up `master.key` from.
- **No host-matching enforcement.** lynxai applies any credential to any URL you ask it to.
- **No retry-on-401 invalidation.** If a `form` session goes stale, lynxai will keep using the cached cookies until it restarts.
