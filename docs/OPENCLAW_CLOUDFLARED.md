# OpenClaw + Cloudflare Tunnel Deployment

This fork keeps a lightweight Palmer deployment path compatible with the previous `codex-as-api` container:

- OpenClaw owns the Codex OAuth profile files.
- Codex2API imports the selected OpenClaw `openai-codex` profile into its normal account store at startup.
- The container can run Codex2API and `cloudflared` in the same image so the host only exposes the container process.

## Environment

The startup importer recognizes these compatibility variables:

```bash
DATABASE_DRIVER=sqlite
DATABASE_PATH=/data/codex2api.db
CACHE_DRIVER=memory
CODEX_AS_API_PORT=18080
CODEX_AS_API_HOST=0.0.0.0
CODEX_AS_API_API_KEY=sk-...
CODEX_AS_API_AUTH_SOURCE=openclaw
CODEX_AS_API_OPENCLAW_AUTH_PATH=/Users/palmer/.openclaw/agents/main/agent/auth-profiles.json
CODEX_AS_API_OPENCLAW_STATE_PATH=/Users/palmer/.openclaw/agents/main/agent/auth-state.json
```

`CODEX_AS_API_API_KEY` is inserted once into the Codex2API API key table if missing. `CODEX_API_KEYS` is also supported for comma-separated keys.

When `CODEX_AS_API_AUTH_SOURCE=openclaw` is set, Codex2API selects the OpenClaw profile in this order:

1. `CODEX_AS_API_OPENCLAW_PROFILE`
2. `auth-state.json` `lastGood["openai-codex"]`
3. `auth-state.json` `order["openai-codex"]`
4. First valid `openai-codex:*` OAuth profile by name

The importer stores the selected profile as a normal Codex2API account. On later starts, it syncs the current OpenClaw tokens into the existing account matched by ChatGPT account ID instead of creating duplicates.

## Cloudflared Image

Build the tunnel image:

```bash
docker build -f Dockerfile.cloudflared -t codex2api-cloudflared:latest .
```

Run it with the OpenClaw auth directory and a Cloudflare tunnel token file mounted read-only:

```bash
docker run \
  --env-file .env \
  -e CLOUDFLARED_TOKEN_FILE=/run/secrets/cloudflared/token \
  -v /path/to/codex2api-data:/data \
  -v /Users/palmer/.openclaw/agents/main/agent:/Users/palmer/.openclaw/agents/main/agent:ro \
  -v /path/to/cloudflared-token-dir:/run/secrets/cloudflared:ro \
  -p 127.0.0.1:18080:18080 \
  codex2api-cloudflared:latest
```

Apple `container` can use the same image, env file, bind mounts, and port mapping. Keep `/data` mounted if you want the SQLite database, imported account metadata, API keys, and usage logs to survive container replacement.
