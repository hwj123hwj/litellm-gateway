# 服务器安全部署指南

适用于：多地点（家/公司/学校）、多设备使用，需要远程访问。

## 前置要求

- Ubuntu/Debian 服务器
- 已有域名，DNS 已解析到服务器 IP
- 已安装 Docker 和 Docker Compose

## 安全架构

```
互联网 ──HTTPS(443)──▶ Nginx ──HTTP(内部)──▶ LiteLLM
                        │                     ├── 智谱
                        │                     ├── 小米
                        │                     └── 美团
                        │
                   三层防护：
                   1. 防火墙：只开 80/443
                   2. HTTPS：传输加密，防抓包
                   3. master_key：身份认证，防白嫖
```

**关键原则**：LiteLLM 的 4000 端口**不对外暴露**，只在内网 Docker 网络中可达。

## 一键部署

### 1. 上传项目到服务器

```bash
# 在本地
scp -r litellm-gateway/ user@your-server:/opt/litellm-gateway/
```

### 2. 初始化部署

SSH 到服务器后执行：

```bash
cd /opt/litellm-gateway
chmod +x scripts/init.sh
./scripts/init.sh ai.yourdomain.com your@email.com
```

脚本会自动完成：
- 创建目录结构
- 签发 Let's Encrypt SSL 证书
- 替换配置中的域名为你的实际域名
- 启动所有 Docker 服务
- 配置防火墙规则

### 3. 填入 API keys

```bash
vi litellm/.env
```

将各提供商的 API key 填入，然后重启：

```bash
docker compose restart litellm
```

### 4. 配置 Claude Code

编辑 `~/.claude/settings.json`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://ai.yourdomain.com/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-your-gateway-key"
  }
}
```

把 `ai.yourdomain.com` 和 `sk-your-gateway-key` 替换为你的实际值。

## 验证

```bash
# HTTPS 访问测试（注意使用 /v1/messages 而不是 /anthropic/v1/messages）
curl https://ai.yourdomain.com/v1/messages \
  -H "x-api-key: sk-your-gateway-key" \
  -H "content-type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"glm-sonnet","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'

# 无 key 应该被拒绝
curl https://ai.yourdomain.com/v1/messages \
  -H "content-type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"glm-sonnet","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'
# 预期返回 401 Unauthorized

# HTTP 应该重定向到 HTTPS
curl -I http://ai.yourdomain.com
# 预期返回 301 → https://
```

## 文件说明

```
/opt/litellm-gateway/
├── docker-compose.yaml     # Docker 编排（Nginx + LiteLLM + PostgreSQL + Certbot）
├── scripts/
│   └── init.sh             # 一键初始化脚本
├── nginx/
│   └── nginx.conf          # Nginx 反代配置
├── litellm/
│   ├── config.yaml         # LiteLLM 模型路由配置
│   ├── longcat_auth.py     # 美团认证 header 重写回调
│   ├── .env.example        # API key 模板
│   └── .env                # 实际 API keys（不提交 git）
└── certbot/
    ├── conf/               # SSL 证书（自动生成）
    └── www/                # 证书验证目录
```

## 配置文件

所有配置文件均已包含在项目中，直接使用即可：

| 文件 | 说明 |
|------|------|
| [docker-compose.yaml](../docker-compose.yaml) | Docker 编排（Nginx + LiteLLM + PostgreSQL + Certbot） |
| [nginx/nginx.conf](../nginx/nginx.conf) | Nginx 反代配置（部署前需替换域名） |
| [litellm/config.yaml](../litellm/config.yaml) | LiteLLM 模型路由配置 |
| [litellm/longcat_auth.py](../litellm/longcat_auth.py) | 美团认证 header 重写回调 |
| [litellm/.env.example](../litellm/.env.example) | API key 模板（复制为 `.env` 并填入实际值） |
| [scripts/init.sh](../scripts/init.sh) | 一键初始化脚本 |

## 安全措施总结

| 措施 | 说明 |
|------|------|
| HTTPS | Let's Encrypt 免费证书，所有传输加密，防公共 WiFi 抓包 |
| master_key | 每个请求必须携带认证 key，防止白嫖额度 |
| LiteLLM 内网隔离 | `expose` 而非 `ports`，4000 端口不对外暴露 |
| 防火墙 | 只开 80/443，其余端口全部关闭 |
| 速率限制 | 30 次/分钟/IP，防止暴力调用 |
| TLS 1.2+ | 禁用旧版本协议 |
| 安全 Headers | HSTS、X-Frame-Options、X-Content-Type-Options |
| .env 权限 | `chmod 600`，仅 owner 可读写 |

## 日常管理

```bash
# 查看所有服务状态
docker compose ps

# 查看日志
docker compose logs -f litellm
docker compose logs -f nginx

# 重启单个服务
docker compose restart litellm

# 更新 LiteLLM
docker compose pull litellm
docker compose up -d litellm

# 证书续期（自动执行，也可手动）
docker compose run --rm certbot renew
docker compose exec nginx nginx -s reload
```
