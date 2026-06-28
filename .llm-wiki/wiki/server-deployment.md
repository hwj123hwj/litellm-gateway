---
type: concept
date: 2026-06-28
tags:
  - deployment
  - cicd
  - server
  - cors
---

# Server Deployment (Updated 2026-06-28)

## 概述

LiteLLM Gateway 部署在 8.141.97.21:4001 服务器上，通过 GitHub Actions CI/CD 自动部署。

## 服务器信息

- **地址：** 8.141.97.21:4001
- **服务：** go-gateway (systemd service)
- **部署目录：** /opt/go-gateway

## CI/CD 配置

### GitHub Actions

文件：`.github/workflows/deploy.yml`

**触发条件：**
- Push to main branch
- Manual workflow_dispatch

**部署目标：**
1. 远程服务器（8.141.97.21）— 通过 SSH 部署
2. Mini PC（self-hosted runner）— 本地部署

### Secrets 配置

| Secret | 说明 |
|--------|------|
| `DEPLOY_HOST` | 部署主机地址 |
| `DEPLOY_USER` | 部署用户 |
| `SSH_PRIVATE_KEY` | SSH 私钥 |
| `LITELLM_MASTER_KEY` | 主密钥 |
| `GLM_API_KEY` | GLM API Key |
| `MIMO_API_KEY` | MiMo API Key |
| `LONGCAT_API_KEY` | LongCat API Key |
| `EASYCLAW_API_KEY` | EasyClaw API Key |

## CORS 配置

### 问题

Android WebView 跨域请求被拒绝，CORS 预检请求返回 401。

### 解决方案

1. **添加 CORS 中间件**（gin-contrib/cors）
   - 允许所有来源跨域请求
   - 允许 Authorization 头
   - 预检请求缓存 12 小时

2. **OPTIONS 请求跳过认证**
   - BearerAuth 中间件跳过 OPTIONS 请求
   - CORS 预检请求不需要携带认证信息

### 测试

```bash
# CORS 预检请求
curl -H "Origin: http://localhost" \
     -H "Access-Control-Request-Method: GET" \
     -H "Access-Control-Request-Headers: Authorization" \
     -X OPTIONS http://8.141.97.21:4001/admin/health

# 返回：
# HTTP/1.1 204 No Content
# Access-Control-Allow-Headers: Origin,Content-Type,Authorization,Accept,X-Requested-With
# Access-Control-Allow-Methods: GET,POST,PUT,PATCH,DELETE,OPTIONS
# Access-Control-Allow-Origin: *
# Access-Control-Max-Age: 43200

# 实际 GET 请求
curl -H "Origin: http://localhost" \
     -H "Authorization: Bearer sk-local-gateway-hwj123hwj" \
     http://8.141.97.21:4001/admin/health

# 返回：
# HTTP/1.1 200 OK
# Access-Control-Allow-Origin: *
# {"status":"ok",...}
```

## 部署流程

1. **测试阶段：**
   - `go vet ./...`
   - `go test -v ./...`

2. **构建阶段：**
   - 交叉编译：`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`
   - 输出：`gateway-linux`

3. **部署阶段：**
   - 上传二进制文件到 /opt/go-gateway
   - 上传配置文件（providers.yaml）
   - 更新 systemd 服务配置
   - 重启服务
   - 健康检查（5次重试）

## 健康检查

```bash
curl http://8.141.97.21:4001/health
# 返回：{"status":"ok"}
```

## Admin API

```bash
curl -H "Authorization: Bearer <MASTER_KEY>" http://8.141.97.21:4001/admin/health
curl -H "Authorization: Bearer <MASTER_KEY>" http://8.141.97.21:4001/admin/dashboard
curl -H "Authorization: Bearer <MASTER_KEY>" http://8.141.97.21:4001/admin/models
curl -H "Authorization: Bearer <MASTER_KEY>" http://8.141.97.21:4001/admin/providers
curl -H "Authorization: Bearer <MASTER_KEY>" http://8.141.97.21:4001/admin/logs
```

## Android APK 配置

在 Android 应用首次启动时配置：
- **后端地址：** `http://8.141.97.21:4001`
- **API Key：** LITELLM_MASTER_KEY 的值
