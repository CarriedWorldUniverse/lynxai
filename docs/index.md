# lynxai

> Self-hostable, AI-native headless browser. The access layer for AI agents in tools where the only door is a human's browser session.

`lynxai` is a free alternative to hosted browser infrastructure (Browserbase, etc.) for AI agents. It runs as a small HTTP server you self-host, with an encrypted credential vault for the sites your agent needs to access, and LLM-driven extraction so agents get clean JSON instead of HTML.

See the [project README](https://github.com/CarriedWorldUniverse/lynxai/blob/main/README.md) for the full project overview, motivation, and license.

## Documentation

For self-hosters and integrators:

- **[Quickstart](usage/quickstart.md)** — install via Docker or `go install`, your first fetch, your first extract, where state lives.
- **[API Reference](usage/api.md)** — all seven endpoints, request/response schemas, examples, and the full error code table.
- **[Credentials](usage/credentials.md)** — the four credential kinds (`basic`, `bearer`, `cookies`, `form`), exact bundle shapes, and how the audit log works.
- **[Operations](usage/operations.md)** — flags, env vars, filesystem layout, backup strategy, security stance, and known limitations in v1.

For contributors and curious readers:

- **[Design Spec](superpowers/specs/2026-05-22-lynxai-v1-design.md)** — the full v1 design.
- **[Implementation Plan](superpowers/plans/2026-05-22-lynxai-v1-implementation.md)** — how v1 was built.
