# Knowledge flywheel runbook

The gateway is the capture and routing layer for the personal AI infrastructure:

```text
AI clients
   │  OpenAI / Responses / Anthropic
   ▼
LLM Gateway ── sanitized conversation_archives ──▶ /admin/archives/export
                                                        │
                         ┌──────────────────────────────┴──────────────────────────────┐
                         ▼                                                             ▼
                 agent-lessons connector                                      OpenWiki gateway connector
                         │                                                             │
                         ▼                                                             ▼
                 knowledge cards + INDEX.md                              ~/.openwiki/wiki Markdown
                         └──────────────────────────────┬──────────────────────────────┘
                                                        ▼
                                           HwjCode `/knowledge search`
```

## Gateway contract

Enable the independent archive data plane with `ARCHIVE_ENABLED=true`. Each
record is redacted before persistence, includes request/provider/status/usage
metadata, preserves stream termination state, and is bounded by
`ARCHIVE_MAX_BODY_KB`. `GET /admin/archives/export` emits deterministic JSONL
and `X-Archive-Next-Cursor`; consumers must persist the cursor only after their
raw page is durable.

`/health` is liveness. `/readyz` is an unauthenticated readiness probe and
returns `503` until at least one routeable provider is loaded. Archive storage
is in the separate `conversation_archives` table and is cleaned by the
retention task.

## Consumer rules

- Use the admin token only through an environment variable; never put it in
  connector config, Markdown, logs, or committed files.
- Keep raw pages and compiler inputs outside Git with user-only permissions.
- Advance the cursor after a successful durable write, not after a successful
  HTTP response alone.
- Treat `truncated=true` or an interrupted status as incomplete evidence; do
  not synthesize a definitive conclusion from a partial conversation.
- Deduplicate by the archive request ID/cursor and preserve the Gateway schema
  version in downstream metadata.

The companion `agent-lessons` repository provides
`scripts/gateway-sync.mjs`, which writes private raw pages and stable compiler
inputs and can invoke the existing `kb-llm-compile.mjs --platform gateway` and
`kb-index.mjs` pipeline. The companion `hwj-wiki` repository provides the
`gateway` OpenWiki connector and the read-only unified search command. HwjCode
can invoke that command through `/knowledge search <query>` or `/kb search`.

## Manual smoke test

```bash
curl -fsS http://127.0.0.1:4001/health
curl -fsS http://127.0.0.1:4001/readyz
curl -sS -H "Authorization: Bearer $ADMIN_TOKEN" \
  'http://127.0.0.1:4001/admin/archives/export?limit=10' \
  -D /tmp/gateway-archive-headers.txt \
  -o /tmp/gateway-archives.jsonl

KB_GATEWAY_URL=http://127.0.0.1:4001 \
KB_GATEWAY_ADMIN_TOKEN="$ADMIN_TOKEN" \
node /path/to/agent-lessons/scripts/gateway-sync.mjs --max-pages 10 --compile

OPENWIKI_AGENT_LESSONS_ROOT=/path/to/agent-lessons \
openwiki search "gateway archive" --json --limit 10
```

The final command is deterministic and read-only: it searches generated
OpenWiki Markdown together with the configured knowledge roots without issuing
another model prompt.
