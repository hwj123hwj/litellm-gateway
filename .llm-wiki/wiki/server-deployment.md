---
type: entity
date: 2026-06-19
tags:
  - deployment
  - server
  - infrastructure
  - systemd
  - aliyun
---

# Server Deployment (8.141.97.21)

## Summary

The production gateway runs on an Alibaba Cloud ECS instance (Ubuntu 24.04) at `8.141.97.21:4001`.

## Access

| Detail | Value |
|--------|-------|
| IP | `8.141.97.21` |
| Port | `4001` |
| User | `root` |
| SSH Key | `~/.ssh/id_ed25519` (user's Mac) |
| CI/CD Key | Dedicated ed25519 keypair (GitHub secret `SSH_PRIVATE_KEY`) |

## Gateway Setup

```bash
# Service
systemctl status go-gateway

# Config
/opt/go-gateway/gateway        # Binary (~31MB)
/opt/go-gateway/providers.yaml # Provider config
/opt/go-gateway/internal/      # Source code

# Service definition
/etc/systemd/system/go-gateway.service
```

## Systemd Service

```
[Unit]
Description=LLM Gateway
After=network.target

[Service]
Type=simple
ExecStart=/opt/go-gateway/gateway
Restart=always          # Auto-restart on crash
RestartSec=5
Environment=PORT=4001
Environment=LOG_LEVEL=info
Environment=LITELLM_MASTER_KEY=...
Environment=GLM_API_KEY=...
# ... other API keys

[Install]
WantedBy=multi-user.target  # Start on boot
```

## Firewall

Alibaba Cloud security group allows inbound TCP `4001` from user's public IP (`103.169.96.128/32`).

## CI/CD

Pushes to GitHub `main` branch trigger [[source-deploy-yml]], which:
1. Runs tests on GitHub Actions
2. Cross-compiles Linux binary
3. SCPs to server
4. Restarts systemd service
5. Health checks at `http://localhost:4001/health`

## History

- **2026-06-19**: Migrated from Docker to native Go binary + systemd. Port changed from 8080 to 4001. New SSH deploy key generated.
- Original: Docker-based via old server with different SSH credentials.

## See Also

- [[source-deploy-yml]] — CI/CD pipeline details
- [[overview]] — Architecture overview
- [[provider-config]] — Provider management
