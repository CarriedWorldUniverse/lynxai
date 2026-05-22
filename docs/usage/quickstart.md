# Quickstart

This walks you from "never heard of lynxai" to "agent fetching pages and extracting JSON" in a few minutes. Pick the Docker path or the binary path — they're equivalent.

## What you need

- An OpenAI-API-compatible LLM key. The default is [DeepSeek](https://platform.deepseek.com/) (cheap, fast, good enough for extraction). Anthropic, OpenAI, or any other OpenAI-compatible endpoint also works — but in v1 the only configurable knob is the API key; the base URL is hardcoded to DeepSeek. See [Operations](operations.md#llm-provider) for the workaround.
- Docker (for the Docker path) or Go 1.26+ and Chromium on PATH (for the binary path).

## Docker path (30 seconds)

```bash
docker run -d -p 7878:7878 \
  -e LYNXAI_LLM_API_KEY=sk-... \
  -v lynxai-data:/data \
  --name lynxai \
  ghcr.io/carriedworlduniverse/lynxai:latest
```

The image bundles Chromium (via `chromedp/headless-shell`), so there's nothing else to install. The `lynxai-data` named volume persists `master.key` and `lynxai.db` across container restarts.

Verify it's up:

```bash
curl http://localhost:7878/healthz
# ok
```

## Binary path

```bash
go install github.com/CarriedWorldUniverse/lynxai/cmd/lynxai@latest
export LYNXAI_LLM_API_KEY=sk-...
lynxai serve
```

Defaults: `--addr 127.0.0.1:7878`, `--data-dir ~/.lynxai`. lynxai will create the data dir (mode `0700`) and a `master.key` file (mode `0600`) on first start.

You'll need Chromium discoverable by chromedp — `chromium-browser`/`chromium` on Linux, `Google Chrome.app` on macOS, etc. See [Operations](operations.md#chromium--browser) for the full list.

Expected startup log:

```
lynxai serving on http://127.0.0.1:7878 (data-dir=/home/you/.lynxai, llm=openai-api/deepseek-chat)
```

## Your first fetch

`/fetch` returns the page rendered as cleaned markdown. Everything is unauthenticated by default — provide a `credential` reference only when you need one.

```bash
curl -sS -X POST http://localhost:7878/fetch \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com"}'
```

Response:

```json
{
  "Markdown": "# Example Domain\n\nThis domain is for use in illustrative examples...",
  "Status": 200,
  "FinalURL": "https://example.com/",
  "Title": "Example Domain"
}
```

!!! note "About `Status`"
    In v1, `Status` is always `200` on a successful navigation. chromedp's `Navigate` doesn't readily expose the real HTTP status; capturing it via a network listener is planned for v1.1. If you need real status codes today, use a non-2xx URL — lynxai will still return `Status: 200` but the page markdown will reflect what the server actually rendered.

By default lynxai strips obvious page chrome (navs, footers, scripts) from the markdown. Pass `"include_chrome": true` if you want the raw conversion.

## Your first extract

`/extract` runs `/fetch` internally, then asks the LLM to emit a JSON object matching your schema. The schema is passed as the LLM tool's input schema, so the model is constrained to match it.

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
              "url":   {"type": "string"},
              "points":{"type": "integer"}
            },
            "required": ["title", "url"]
          }
        }
      },
      "required": ["stories"]
    }
  }'
```

Response:

```json
{
  "json": {"stories":[{"title":"...","url":"...","points":123}, ...]},
  "status": 200,
  "final_url": "https://news.ycombinator.com/"
}
```

The `json` field is validated against the schema you provided before being returned. If the LLM emits something that doesn't conform, you get an `extraction_failed` error.

## Storing your first credential

Credentials are referenced by name and scoped to a `host`. Bundle data never comes back out — clients reference credentials by name only.

Store a bearer token:

```bash
curl -sS -X POST http://localhost:7878/credentials \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "github-mine",
    "kind": "bearer",
    "host": "api.github.com",
    "bundle": {"host":"api.github.com","token":"ghp_xxxxxxxxxxxx"}
  }'
# {"name":"github-mine"}
```

Use it:

```bash
curl -sS -X POST http://localhost:7878/fetch \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://api.github.com/user",
    "credential": {"name":"github-mine"}
  }'
```

The bearer token is applied as an `Authorization: Bearer ghp_...` header before navigation. Every use writes one audit row (name, request URL, outcome) — see [Credentials](credentials.md#audit-log).

List what's stored:

```bash
curl -sS http://localhost:7878/credentials
# [{"name":"github-mine","kind":"bearer","host":"api.github.com","created_at":"...","updated_at":"..."}]
```

Delete:

```bash
curl -sS -X DELETE http://localhost:7878/credentials/github-mine
```

## Where state lives

| Path                          | What                                            | Perms  |
|-------------------------------|-------------------------------------------------|--------|
| `<data-dir>/master.key`       | 32-byte AES key material (random on first run)  | `0600` |
| `<data-dir>/lynxai.db`        | SQLite — encrypted bundles + audit log          | `0600` |

Default `<data-dir>` is `~/.lynxai` for the binary, `/data` for the Docker image. Override with `--data-dir` or `LYNXAI_DATA_DIR`.

!!! warning "Master key is your only key"
    If you lose `master.key`, every credential in `lynxai.db` is unrecoverable — AES-256-GCM authenticated encryption means there's no rescue path. Back up `master.key` **and** `lynxai.db` together. Backing up one without the other is useless.

!!! warning "Do not share the data dir"
    `master.key` is the credential vault root. Don't commit it. Don't sync it through Dropbox/iCloud unencrypted. Don't `chmod` it to anything more permissive than `0600`. Don't run two lynxai instances against the same data-dir — SQLite without WAL won't enjoy that.

## Next steps

- [API Reference](api.md) — every endpoint, request/response schema, and error code.
- [Credentials](credentials.md) — the four credential kinds (`basic`, `bearer`, `cookies`, `form`), exact bundle shapes, and how the audit log works.
- [Operations](operations.md) — flags, env vars, backup strategy, security stance, and the known limitations in v1.
