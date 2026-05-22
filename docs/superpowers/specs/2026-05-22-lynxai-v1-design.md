# lynxai — v1 Design

**Date:** 2026-05-22
**Status:** Draft for review
**License:** Apache-2.0
**Language:** Go (single static binary)
**Repo:** `github.com/CarriedWorldUniverse/lynxai`
**Depends on:** [`bridle`](https://github.com/CarriedWorldUniverse/bridle) (LLM harness — provider abstraction, tool calling, streaming)

## Summary

`lynxai` is a self-hostable, open-source, AI-native headless browser. It is a free
alternative to Browserbase aimed at people who can't or won't pay for hosted
browser infrastructure. Spirit: lynx (text-first, scriptable, agent-friendly).
Capabilities: Browserbase-shaped (managed Chromium, AI extraction, credential
handling), grown over time.

**Designed to be driven by AI.** The primary consumer is an LLM agent calling
lynxai's tools to fetch, authenticate, and extract — not a human curling
endpoints. The surface, the response shapes, and the error model are all tuned
for that consumer: returns are markdown (not DOM), errors carry agent-actionable
detail, every tool is single-call with no hidden state.

**Internal AI is provided by bridle.** lynxai does not ship its own LLM client.
It depends on `bridle`, the project's Go harness library, which owns the
provider abstraction (`claude-api`, `ollama-local`, `openai-api`), tool calling,
and streaming. lynxai's `extract` runs as a single-turn bridle invocation with
the caller's schema as the tool input schema — bridle handles provider
selection, credentials, retries, and cost accounting.

This document specifies **v1 only**. v1 is deliberately minimal: enough to be
useful for the most common agent task (fetch a page, optionally authenticated,
optionally extract structured data) and no more. The rest is scoped on top in
later specs.

## Non-goals (v1)

These belong to later specs and are explicitly out of scope here:

- `act` / `observe` AI primitives (click-by-description, page understanding)
- Sessions as a first-class HTTP resource (`POST /sessions`, CDP-over-websocket relay)
- Persisted contexts (cookie/storage jars decoupled from credentials)
- Stealth / anti-detection patches
- Proxy configuration
- File downloads / uploads
- Live view / debugging UI
- CLI frontend beyond `lynxai serve`
- Multi-tenant operation (auth on the API surface, quotas, billing)
- OAuth credential kind (refresh flows aren't "text-based" in the v1 sense)
- Shipping our own LLM provider client (bridle owns this; lynxai is provider-agnostic
  through bridle's `ProviderID`)

## v1 deliverable

A single Go static binary, `lynxai`, with one subcommand that matters:

```
lynxai serve --addr :7878 --data-dir ~/.lynxai
```

Exposes two REST endpoints over plain HTTP (no auth in v1 — self-host on
loopback or behind your own reverse proxy):

- `POST /fetch` — navigate to a URL, return the page as cleaned markdown
- `POST /extract` — navigate to a URL, return JSON conforming to a caller-supplied schema

Plus a credential management surface (CRUD over REST) for the encrypted vault.

## Architecture

```
                  ┌──────────────────────┐
                  │   HTTP API (chi)     │  POST /fetch, /extract, /credentials/*
                  └──────────┬───────────┘
                             │
        ┌────────────────────┼─────────────────────┐
        │                    │                     │
        ▼                    ▼                     ▼
┌───────────────┐   ┌────────────────┐   ┌──────────────────┐
│    engine     │   │    extract     │   │      creds       │
│ (chromedp +   │   │ (bridle turn   │   │ (encrypted       │
│  html→md)     │   │  with schema   │   │  SQLite vault)   │
│               │   │  as tool)      │   │                  │
└───────┬───────┘   └────────┬───────┘   └─────────┬────────┘
        │                    │                     │
        ▼                    ▼                     ▼
┌───────────────┐   ┌────────────────┐   ┌──────────────────┐
│ headless      │   │     bridle     │   │      audit       │
│ Chromium      │   │  (external Go  │   │  (per-cred-use   │
│ (system or    │   │   dep; owns    │   │   log row)       │
│  downloaded)  │   │   providers)   │   │                  │
└───────────────┘   └────────────────┘   └──────────────────┘
```

Each box is one Go package under `internal/`. Boundaries are real: `engine`
doesn't know about `extract` or `creds`; `creds` doesn't know about HTTP; the
`api` package wires them together.

## Repository layout

```
cmd/lynxai/main.go             # CLI entry, `serve` subcommand only in v1
internal/api/                  # REST handlers, request/response types
internal/engine/               # chromedp wrapper, render to markdown
internal/extract/              # schema-driven extraction via bridle
internal/creds/                # encrypted SQLite vault, bundle validators, audit
docs/superpowers/specs/        # design specs (this file)
LICENSE                        # Apache-2.0
README.md                      # project overview, install, quickstart
go.mod / go.sum
```

## Components

### `internal/engine` — page fetch and render

**Responsibility:** given a URL and optional pre-applied credential, drive a
headless Chromium instance to load the page and return cleaned markdown plus
metadata (final URL after redirects, HTTP status, page title).

**Implementation:**

- `chromedp` (CDP-direct, pure Go) as the driver
- Chromium binary: prefer system-installed (PATH lookup); fall back to
  downloading a pinned revision to `~/.lynxai/chromium/` on first run
- A small `chromedp.Allocator` pool (default size 4, configurable) — Chromium
  startup is the main fetch latency; reuse pays for itself after the first call
- Wait strategy: `networkidle` with a 10 s hard cap. Configurable per request.
- After load: pull outer HTML, run through `html-to-markdown` (the Go library
  `JohannesKaufmann/html-to-markdown`), with rules that strip `<script>`,
  `<style>`, `<noscript>`, `<svg>` and common chrome (`<nav>`, `<header>`,
  `<footer>`) by default. Configurable per request via an `include_chrome` flag.
- Credential application happens **before** navigation:
  - `KindBasic` / `KindBearer`: register a CDP `Network.setExtraHTTPHeaders`
    scoped to the matching host
  - `KindCookies`: `Network.setCookies` for the credential's cookie list
  - `KindForm`: see "Form login" below
- Each fetch runs in its own browser **context** (not a full process) for cookie
  isolation between unrelated requests. Browser process is reused.

**Public API (Go):**

```go
type FetchRequest struct {
    URL         string
    Credential  *CredentialRef // nil = unauthenticated
    IncludeChrome bool
    Timeout     time.Duration
}

type FetchResult struct {
    Markdown string
    Status   int
    FinalURL string
    Title    string
}

func (e *Engine) Fetch(ctx context.Context, req FetchRequest) (*FetchResult, error)
```

### Form login (sub-component of `engine`)

For `KindForm` credentials, on first use within a process lifetime:

1. POST directly to `bundle.login_url` with `bundle.fields` as form-encoded body
   (driver: a stdlib `http.Client` with a cookie jar, **not** Chromium — login
   POSTs are pure HTTP and don't need a render)
2. Watch for `bundle.success_cookie_name` in the response's `Set-Cookie`
3. On success, cache the resulting cookies in an in-memory map keyed by
   credential name. On failure, return a `CredentialError` with the HTTP status
   and a one-line body excerpt.
4. Subsequent fetches with the same credential seed those cookies into the
   browser context via `Network.setCookies` before navigation.

The cache lives only for the lifetime of the `lynxai serve` process. Persisted
contexts come in a later spec.

### `internal/extract` — schema-driven extraction

**Responsibility:** given a URL, optional credential, and a JSON Schema, return
JSON that conforms to the schema, extracted from the page content.

**Implementation:**

- Call `engine.Fetch` to get markdown
- Build a bridle `TurnRequest`:
  - `SystemPrompt`: short fixed prompt instructing the model to extract data
    matching the supplied schema from the page content
  - `UserMessage`: the page markdown
  - `Tools`: exactly one tool, `emit_extraction`, whose input schema is the
    caller's JSON Schema verbatim. The model is told to call this tool once.
  - `Provider` / `Model`: read from the bridle config passed in at server
    start (see "bridle dependency" below) — lynxai does not duplicate
    provider/model flags. Bridle's defaults apply if unset.
  - `MaxSteps`: 1 (one tool round; no agentic loop in extract)
- Call `bridle.Harness.RunTurn`. The model's tool call carries the structured
  JSON matching the schema — bridle's provider layer guarantees the call shape
  conforms via the provider's native structured-output mechanism (Anthropic
  tool use, OpenAI tool/function calling, etc.).
- Take the `ToolInvocation` args from the `TurnResult`. Validate against the
  schema as a belt-and-braces check; return `ExtractError` if it doesn't
  validate.
- Discard the streamed events in v1 (no event sink consumer). They become
  useful when we add a `drive` endpoint in a later spec.

**Public API (Go):**

```go
type ExtractRequest struct {
    URL         string
    Credential  *CredentialRef
    Schema      json.RawMessage // JSON Schema
    Timeout     time.Duration
}

type ExtractResult struct {
    JSON     json.RawMessage
    Status   int
    FinalURL string
}

func (x *Extractor) Extract(ctx context.Context, req ExtractRequest) (*ExtractResult, error)
```

The LLM credential is **not** one of the per-site web credentials and is **not**
managed by the lynxai vault — bridle owns provider credentials. lynxai's
`serve` command accepts `--bridle-config <path>` pointing at a bridle config
file (or `LYNXAI_BRIDLE_CONFIG` env), and passes it through. Bridle's existing
credential handling does the rest, which means lynxai inherits whatever
providers bridle supports without code changes here.

### `internal/creds` — encrypted credential vault

**Responsibility:** store, retrieve, and audit credentials. Apply encryption at
rest. Validate bundles on write.

**Storage:** SQLite at `<data_dir>/lynxai.db`. One table for credentials, one
for audit:

```sql
CREATE TABLE credentials (
  name        TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,           -- basic | bearer | cookies | form
  host        TEXT NOT NULL,           -- scope; matched as suffix on request host
  bundle      BLOB NOT NULL,           -- AES-256-GCM ciphertext of bundle JSON
  nonce       BLOB NOT NULL,           -- per-row 12-byte nonce
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

CREATE TABLE credential_audit (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  used_at     INTEGER NOT NULL,
  request_url TEXT NOT NULL,
  outcome     TEXT NOT NULL            -- ok | not_found | decrypt_failed | apply_failed
);
```

**Encryption:** AES-256-GCM. Data key derived via HKDF-SHA256 from a master key
stored at `<data_dir>/master.key` (generated on first start, 0600 perms). Info
string: `"lynxai.credentials.v1"`. This mirrors the proven shape used in
`nexus/credentials` — same cryptographic choices, different deployment.

**Bundle shapes (text-based credentials only in v1):**

```jsonc
// kind=basic
{ "host": "api.example.com", "user": "alice", "password": "..." }

// kind=bearer
{ "host": "api.example.com", "token": "..." }

// kind=cookies
{
  "host": "example.com",
  "cookies": [
    { "name": "sid", "value": "...", "domain": ".example.com", "path": "/",
      "secure": true, "http_only": true }
  ]
}

// kind=form
{
  "host": "example.com",
  "login_url": "https://example.com/login",
  "method": "POST",
  "fields": {
    "user_field": "username", "pass_field": "password",
    "user": "alice", "password": "..."
  },
  "success_cookie_name": "sessionid"
}
```

Each kind has a validator function that runs on every write. Unknown kinds are
rejected. Bundle JSON is encrypted before persisting; the plaintext never
touches disk.

**Audit:** every successful or failed credential application writes one row.
`request_url` is the *request* URL, not the credential bundle. No bundle data
is ever written to the audit table.

**Public API (Go):**

```go
type Vault interface {
    Put(ctx context.Context, name string, kind Kind, host string, bundle []byte) error
    Get(ctx context.Context, name string) (*Credential, error)
    List(ctx context.Context) ([]CredentialSummary, error) // no bundles
    Delete(ctx context.Context, name string) error
    RecordUse(ctx context.Context, name, requestURL, outcome string) error
}
```

### bridle dependency (external)

bridle is imported as a Go module (`github.com/CarriedWorldUniverse/bridle`).
lynxai uses exactly one function from it:

```go
func (h *Harness) RunTurn(ctx context.Context, req TurnRequest, sink EventSink) (TurnResult, error)
```

Constructed once at server start with the bridle config, reused across all
`extract` requests. Event sink in v1 is a no-op (`bridle.DiscardSink`).
Provider, model, and provider credentials are bridle's concern, configured
through bridle's own config surface — lynxai does not duplicate that surface.

### `internal/api` — REST surface

**Endpoints:**

| Method | Path                       | Body / response                        |
|--------|----------------------------|----------------------------------------|
| POST   | `/fetch`                   | FetchRequest → FetchResult             |
| POST   | `/extract`                 | ExtractRequest → ExtractResult         |
| POST   | `/credentials`             | CredentialPut (name, kind, host, bundle) |
| GET    | `/credentials`             | [CredentialSummary]                    |
| GET    | `/credentials/{name}`      | CredentialSummary (no bundle)          |
| DELETE | `/credentials/{name}`      | 204                                    |
| GET    | `/healthz`                 | 200 ok                                 |

Request/response bodies are JSON. `credential` in fetch/extract requests is
`{"name": "..."}` referencing a stored credential by name; the bundle never
crosses the API boundary on use, only on initial storage.

No authentication on the API in v1 — bind to loopback by default
(`--addr 127.0.0.1:7878`). Self-hosters who want to expose it put it behind
their own reverse proxy with auth.

## Data flow: a fetch with form-login credentials

1. Client: `POST /fetch { "url": "https://example.com/dashboard", "credential": {"name": "example-prod"} }`
2. `api` validates request, looks up credential by name via `creds.Vault`
3. `creds.Vault` decrypts the bundle, returns it to `api`
4. `api` calls `engine.Fetch` with the credential
5. `engine` checks its in-memory form-login cache for `example-prod`:
   - **miss**: POST to `login_url` with fields, capture cookies, store in cache
   - **hit**: use cached cookies
6. `engine` opens a fresh browser context, sets the cookies via CDP, navigates
   to the URL with `networkidle` wait
7. `engine` pulls outer HTML, converts to markdown
8. `engine` calls `creds.Vault.RecordUse` with outcome `ok` (or failure)
9. `api` returns the FetchResult JSON

Failure modes write audit rows with the corresponding `outcome` value.

## Error handling

Errors are classified and surfaced as structured JSON:

```json
{ "error": { "code": "credential_not_found", "message": "...", "details": {} } }
```

Error codes for v1:

| Code                       | When                                                  | HTTP |
|----------------------------|-------------------------------------------------------|------|
| `bad_request`              | malformed JSON, missing required field, bad schema    | 400  |
| `credential_not_found`     | referenced credential doesn't exist                   | 404  |
| `credential_decrypt_failed`| stored ciphertext won't decrypt (likely key mismatch) | 500  |
| `credential_apply_failed`  | form login returned non-success / no success cookie   | 502  |
| `navigation_failed`        | Chromium failed to load the URL (timeout, DNS, TLS)   | 502  |
| `extraction_failed`        | LLM returned no JSON, or JSON didn't validate         | 502  |
| `llm_unavailable`          | LLM provider call failed (network, 5xx, rate limit)   | 503  |
| `internal_error`           | everything else                                       | 500  |

No retries inside `lynxai` — retries are a caller concern. The only exception:
LLM provider 429s get one transparent retry with exponential backoff, because
that's a provider quirk, not a caller decision.

## Testing

**Unit tests** for every package:

- `creds`: round-trip encrypt/decrypt; bundle validation per kind; audit rows
  are written; key derivation is deterministic
- `engine`: markdown conversion rules (table-driven, fixed HTML inputs); cookie
  injection; form-login cache hit/miss; header injection per credential kind
- `extract`: schema validation; bridle is mocked behind a small local interface
  (a `Turner` with one method matching `RunTurn`'s signature) so tests don't
  spin up a real bridle harness
- `api`: handler unit tests with the three dependencies mocked

**Integration tests** (gated behind a `-tags integration` build tag so they
don't run by default — they need Chromium and network):

- Fetch a known static page (a fixture HTML served from a `httptest.Server`),
  assert markdown output
- Fetch with basic auth against `httptest.Server` requiring it, assert success
- Form login flow against `httptest.Server` simulating a login endpoint
- Extract against a fixture page with a small schema, asserting the JSON shape
  (bridle uses a real provider — `ollama-local` if available, falling back to
  `claude-api` gated by env var, skipped without either)

**No** end-to-end tests against live third-party sites in v1 — they're flaky and
make CI a liability.

## Out-of-scope, but worth noting now

These items are explicitly **not in v1** and have their own future specs:

- **Sessions spec** — first-class session resources, CDP-over-websocket relay so
  clients (Stagehand, Playwright, lynxai CLI) can drive the browser directly
- **Contexts spec** — persisted cookie/storage jars, separate from credentials
- **AI primitives spec** — `act`, `observe` (Stagehand-shape), agentic
  multi-step browsing. Naturally implemented as a bridle multi-step turn
  (`MaxSteps > 1`) where each step's tools are lynxai actions
- **Drive endpoint spec** — `POST /drive { goal, tools, credential? }`: a
  bridle turn whose tool surface is lynxai's own actions. The endpoint where
  "we drive it via AI" becomes a first-class operation rather than something
  the caller assembles
- **Stealth spec** — anti-detection patches, fingerprint management
- **Proxies spec** — per-request and per-context proxy configuration
- **CLI spec** — `lynxai browse <url>` interactive lynx-style frontend
- **MCP spec** — `nexus-web-mcp` wrapper exposing lynxai to nexus aspects

Each will be its own design doc when its turn comes. v1's job is to make sure
the engine, creds, and extract boxes have boundaries clean enough that those
specs add new packages and endpoints rather than rewriting these ones.

## Open questions (none blocking v1)

- Do we ship a Docker image alongside the binary release in v1? (Probably yes,
  for self-hosters who'd rather `docker run` than manage Go installs. Single
  Dockerfile, ~20 lines.) Decide before first release, not now.
- Master-key rotation story — not v1, but should be designed before the vault
  has long-lived production data in it. Add a key-rotation spec when there's a
  user who needs it.
