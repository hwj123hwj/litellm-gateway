---
type: source
source_path: .github/workflows/deploy.yml
date: 2026-06-19
tags:
  - ci-cd
  - deployment
  - github-actions
  - systemd
---

# Source: deploy.yml (CI/CD Pipeline)

## Summary

GitHub Actions workflow that tests, builds, and deploys the gateway to `8.141.97.21:4001` on every push to `main`.

## Pipeline Stages

```
git push main → [Test] → [Cross-compile] → [Deploy to Server] → [systemd restart]
```

## Stage Details

### 1. Test
- `go vet ./...`
- `go test -v ./...`
- Runs before deploy, blocks on failure

### 2. Build
- `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o gateway-linux`
- Compiles ~31MB static binary

### 3. Deploy
- SCP binary to `/opt/go-gateway/gateway-linux.new`
- SCP `providers.yaml` and `internal/` to server
- Atomic swap: `mv gateway-linux.new gateway`
- Regenerates systemd service with fresh secrets
- `systemctl daemon-reload && systemctl restart go-gateway`
- Health check: `curl -sf http://localhost:4001/health`

## Secrets Required

| Secret | Purpose |
|--------|---------|
| `SSH_PRIVATE_KEY` | ed25519 deploy key (generated 2026-06-19) |
| `DEPLOY_HOST` | `8.141.97.21` |
| `DEPLOY_USER` | `root` |
| `LITELLM_MASTER_KEY` | Gateway auth token |
| `GLM_API_KEY` | Zhipu |
| `MIMO_API_KEY` | Xiaomi |
| `LONGCAT_API_KEY` | Meituan |
| `EASYCLAW_API_KEY` | Claude proxy |
| `OPENROUTER_API_KEY` | GPT + free models |

## Key Design Decisions

- **No Docker**: Server in China can't pull Docker Hub images. Uses native Go binary + systemd instead.
- **Atomic deploy**: Binary is uploaded as `.new`, then atomically swapped with `mv` — zero downtime.
- **Explicit SSH identity**: Uses `-i ~/.ssh/deploy_key` explicitly, not relying on default SSH behavior.
- **Secret injection**: API keys are embedded into systemd `Environment=` lines during deploy.

## History

- **2026-06-19**: Rewrote from Docker-based to native Go + systemd. Fixed SSH key (was configured for old server). Changed port from 8080 to 4001.
- Original: Docker-based build on server, port 8080, old server IP.

## See Also

- [[server-deployment]] — Server setup details
- [[overview]] — Architecture overview
- [[provider-config]] — Provider management
