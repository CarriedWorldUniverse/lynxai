# API Reference

lynxai v1 exposes seven HTTP endpoints. All bodies are JSON. All responses are JSON except `/healthz`, which is `text/plain`.

There is **no built-in authentication** in v1. Bind to loopback (the default `127.0.0.1:7878`) or put lynxai behind a reverse proxy that enforces auth. See [Operations](operations.md#security-stance).

## Conventions

- Request bodies are `Content-Type: application/json`.
- Field names are exactly as documented — they match the on-the-wire JSON, not the Go struct names.
- Error responses have a uniform shape:

```json
{
  "error": {
    "code": "bad_request",
    "message": "url required",
    "details": null
  }
}
```

The full error code table is at [the bottom of this page](#error-codes).

---

## `GET /healthz`

Liveness probe. Returns `200 ok\n` if the server is up. Doesn't touch the DB, browser, or LLM — use it for container health checks, not full-stack readiness.

```bash
curl -sS http://localhost:7878/healthz
# ok
```

**Response:** `200 OK`, body `ok\n` (text/plain).

**Errors:** none — if the process is alive, this returns 200.

---

## `POST /credentials`

Store (or replace) a credential. Names are unique; storing a credential with a name that already exists overwrites the previous bundle.

### Request

| Field    | Type     | Required | Description                                                        |
|----------|----------|----------|--------------------------------------------------------------------|
| `name`   | string   | yes      | Unique identifier used by `/fetch` and `/extract`.                 |
| `kind`   | string   | yes      | One of `basic`, `bearer`, `cookies`, `form`.                       |
| `host`   | string   | yes      | Host the credential is scoped to (e.g. `api.github.com`).          |
| `bundle` | object   | yes      | Kind-specific bundle. See [Credentials](credentials.md) for shapes.|

### Response

`201 Created`:

```json
{"name": "github-mine"}
```

### Example

```bash
curl -sS -X POST http://localhost:7878/credentials \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "github-mine",
    "kind": "bearer",
    "host": "api.github.com",
    "bundle": {"host":"api.github.com","token":"ghp_xxxxxxxxxxxx"}
  }'
```

### Errors

- `bad_request` (400) — missing field, invalid JSON, or bundle that fails kind validation (e.g. a `basic` bundle without `password`).
- `internal_error` (500) — vault write failure.

---

## `GET /credentials`

List all stored credentials. **Bundles are never returned** — only summaries.

### Response

`200 OK`. Array of summaries (empty array if none):

```json
[
  {
    "name": "github-mine",
    "kind": "bearer",
    "host": "api.github.com",
    "created_at": "2026-05-22T10:30:00Z",
    "updated_at": "2026-05-22T10:30:00Z"
  }
]
```

### Example

```bash
curl -sS http://localhost:7878/credentials
```

### Errors

- `internal_error` (500) — vault read failure.

---

## `GET /credentials/{name}`

Fetch one credential summary by name. Like `GET /credentials`, this returns no bundle data.

### Response

`200 OK`:

```json
{
  "name": "github-mine",
  "kind": "bearer",
  "host": "api.github.com",
  "created_at": "2026-05-22T10:30:00Z",
  "updated_at": "2026-05-22T10:30:00Z"
}
```

### Example

```bash
curl -sS http://localhost:7878/credentials/github-mine
```

### Errors

- `credential_not_found` (404) — no credential with that name.
- `internal_error` (500) — vault read failure.

---

## `DELETE /credentials/{name}`

Remove a credential. Audit rows for the credential are **not** deleted — they remain in the audit log even after the credential is gone.

### Response

`204 No Content` (empty body).

### Example

```bash
curl -sS -X DELETE http://localhost:7878/credentials/github-mine
```

### Errors

- `credential_not_found` (404) — no credential with that name.
- `internal_error` (500) — vault write failure.

---

## `POST /fetch`

Navigate to a URL with a headless Chromium instance and return the page as cleaned markdown.

### Request

| Field            | Type    | Required | Description                                                                                           |
|------------------|---------|----------|-------------------------------------------------------------------------------------------------------|
| `url`            | string  | yes      | URL to fetch.                                                                                         |
| `credential`     | object  | no       | `{"name": "..."}` — references a stored credential. Bundle is resolved server-side.                   |
| `include_chrome` | boolean | no       | If `true`, keep page chrome (navs, footers, scripts) in the markdown. Default `false`.                |

### Response

`200 OK`:

```json
{
  "Markdown": "# Example Domain\n\nThis domain is for use in illustrative examples...",
  "Status": 200,
  "FinalURL": "https://example.com/",
  "Title": "Example Domain"
}
```

| Field      | Type    | Description                                                                          |
|------------|---------|--------------------------------------------------------------------------------------|
| `Markdown` | string  | Cleaned (or raw, with `include_chrome`) markdown rendering of the page.              |
| `Status`   | integer | HTTP status of the page. **v1 limitation: always `200` on successful navigation.**   |
| `FinalURL` | string  | Final URL after any redirects.                                                       |
| `Title`    | string  | The `<title>` of the rendered page.                                                  |

!!! warning "v1: `Status` is hardcoded to 200"
    `chromedp.Navigate` doesn't readily surface the HTTP status, so the engine returns `200` for any navigation that didn't error out. Capturing the real status via a network listener is planned for v1.1.

### Examples

Unauthenticated:

```bash
curl -sS -X POST http://localhost:7878/fetch \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com"}'
```

With a stored credential:

```bash
curl -sS -X POST http://localhost:7878/fetch \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://api.github.com/user",
    "credential": {"name":"github-mine"}
  }'
```

### Errors

- `bad_request` (400) — missing or invalid `url`, malformed JSON body.
- `credential_not_found` (404) — `credential.name` doesn't exist. Writes an audit row with outcome `not_found`.
- `credential_decrypt_failed` (500) — vault decrypt failed (corrupted DB or wrong master key). Writes an audit row with outcome `decrypt_failed`.
- `credential_apply_failed` (502) — only fires for `form` credentials when the login POST fails or the success cookie isn't seen. Writes an audit row with outcome `apply_failed`.
- `navigation_failed` (502) — Chromium failed to navigate (timeout, DNS failure, page crash). If a credential was supplied, writes an audit row with outcome `ok` because the credential itself was applied successfully — only the page fetch failed.
- `internal_error` (500) — other server-side failure.

---

## `POST /extract`

Fetch a URL and run an LLM-driven structured extraction against the resulting markdown. The LLM is given your JSON Schema as a single tool's input schema and instructed to call that tool exactly once.

### Request

| Field            | Type    | Required | Description                                                                          |
|------------------|---------|----------|--------------------------------------------------------------------------------------|
| `url`            | string  | yes      | URL to fetch.                                                                        |
| `schema`         | object  | yes      | JSON Schema describing the extraction shape. Becomes the LLM tool's input schema.    |
| `credential`     | object  | no       | `{"name": "..."}` — references a stored credential.                                  |
| `include_chrome` | boolean | no       | If `true`, include page chrome in the markdown fed to the LLM. Default `false`.      |

### Response

`200 OK`:

```json
{
  "json":      {"stories": [{"title": "...", "url": "..."}]},
  "status":    200,
  "final_url": "https://news.ycombinator.com/"
}
```

| Field       | Type    | Description                                                                |
|-------------|---------|----------------------------------------------------------------------------|
| `json`      | object  | The extraction. Validated against `schema` before being returned.          |
| `status`    | integer | HTTP status (same v1 limitation as `/fetch` — always `200`).               |
| `final_url` | string  | Final URL after any redirects.                                             |

### Example

```bash
curl -sS -X POST http://localhost:7878/extract \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://news.ycombinator.com",
    "schema": {
      "type": "object",
      "properties": {
        "stories": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "title": {"type": "string"},
              "url":   {"type": "string"}
            },
            "required": ["title", "url"]
          }
        }
      },
      "required": ["stories"]
    }
  }'
```

### How it works

1. lynxai runs the same `/fetch` pipeline (with the credential, if supplied).
2. The resulting markdown is sent to the LLM as the user message, prefixed with a system prompt instructing it to call `emit_extraction` exactly once.
3. `emit_extraction` is declared with your `schema` as its input schema, so the model is constrained to match.
4. The tool call's arguments are validated against `schema` and returned in `json`.

The tool name `emit_extraction` is fixed in v1. The bridle harness runs at most one step (`MaxSteps: 1`).

### Errors

- `bad_request` (400) — missing `url` or `schema`, malformed JSON body.
- `credential_not_found` (404), `credential_decrypt_failed` (500), `credential_apply_failed` (502) — same as `/fetch`.
- `navigation_failed` (502) — Chromium failed to navigate.
- `extraction_failed` (502) — LLM call failed, returned no tool call, or returned arguments that didn't validate against `schema`. **v1 limitation: LLM provider unavailability also reports as `extraction_failed` rather than `llm_unavailable` (503).**
- `internal_error` (500) — other server-side failure.

---

## Error codes

All non-2xx responses use the structured shape:

```json
{"error": {"code": "<code>", "message": "<description>", "details": null}}
```

| Code                          | HTTP | When it fires                                                                                   |
|-------------------------------|------|-------------------------------------------------------------------------------------------------|
| `bad_request`                 | 400  | Malformed JSON, missing required field, invalid bundle shape.                                   |
| `credential_not_found`        | 404  | `credential.name` (in a fetch/extract) or path param (in GET/DELETE) doesn't match any record.  |
| `credential_decrypt_failed`   | 500  | Vault read succeeded but AES-GCM open failed (wrong master key, corrupted ciphertext).          |
| `credential_apply_failed`     | 502  | Form-login POST failed or success cookie not seen. Does **not** fire for navigation failures.   |
| `navigation_failed`           | 502  | Chromium failed to load the page (timeout, DNS, crash, etc.).                                   |
| `extraction_failed`           | 502  | LLM returned no tool call, returned malformed args, or the upstream provider errored out.       |
| `llm_unavailable`             | 503  | Reserved. Not currently emitted in v1 — LLM provider errors come back as `extraction_failed`.   |
| `internal_error`              | 500  | Anything else (vault write failure, unknown credential kind, etc.).                             |
