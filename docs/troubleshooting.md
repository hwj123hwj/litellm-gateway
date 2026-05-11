# LiteLLM 启动问题排查记录

## 问题 1：环境变量未加载

### 症状
```
docker logs litellm
# LITELLM_MASTER_KEY= (为空)
```
请求返回 `400 Bad Request` 或 `401 Unauthorized`

### 原因
使用 `--env-file` 加载 .env 文件时，环境变量未正确传递到容器内。

### 解决方案
使用 `-e` 直接传递环境变量：

```bash
docker run -d \
  --name litellm \
  --network host \
  -v ~/.litellm/config.yaml:/app/config.yaml \
  -v ~/.litellm/longcat_auth.py:/app/longcat_auth.py \
  -e LITELLM_MASTER_KEY=sk-local-gateway-hwj123hwj \
  -e GLM_API_KEY=your-glm-key \
  -e MIMO_API_KEY=your-mimo-key \
  -e LONGCAT_API_KEY=your-longcat-key \
  ghcr.io/berriai/litellm:main-latest \
  --config /app/config.yaml
```

---

## 问题 7：EasyClaw 模型参数不兼容

### 症状
Claude Code 请求 EasyClaw 时报错：
```
litellm.UnsupportedParamsError: openai does not support parameters: ['thinking', 'context_management']
```

### 原因
Claude Code 发送请求时包含 Anthropic 特有参数（`thinking`、`context_management`），这些在 OpenAI 格式中不被识别。

### 解决方案

EasyClaw 实际上**接受但忽略**这些参数，只需启用 `drop_params`：

```yaml
- model_name: claude-sonnet-4-6
  litellm_params:
    model: openai/claude-sonnet-4-6
    api_base: https://api.easyclaw.work
    api_key: os.environ/EASYCLAW_API_KEY
    drop_params: true
```

### 功能影响

| 功能 | 影响 | 说明 |
|------|------|------|
| Extended Thinking | ❌ 不可用 | EasyClaw 不支持 |
| 上下文管理 | ✅ 正常 | EasyClaw 有自己的机制 |
| Streaming | ✅ 正常 | 完全支持 |
| Tool Use | ✅ 正常 | 完全支持 |

### 验证
```bash
curl -X POST http://localhost:4000/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-hwj123hwj" \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-sonnet-4-6", "messages": [{"role": "user", "content": "Hi"}], "max_tokens": 50}'
```

重启容器生效：`docker restart litellm`


---

## 问题 6：美团 LongCat 返回 missing_api_key

### 症状
```
美团 LongCat API 返回 `missing_api_key` 错误
```

### 原因
- LiteLLM 对 `anthropic/` 前缀的模型默认发送 `x-api-key` header
- 美团 API 只接受 `Authorization: Bearer <token>` 格式

### 解决方案

#### 步骤 1：确认文件存在
```bash
docker exec litellm ls -la /app/longcat_auth/longcat_auth.py
```

#### 步骤 2：确认回调代码可以正常导入
```bash
docker exec litellm python -c "import sys; sys.path.insert(0, '/app'); from longcat_auth.longcat_auth import LongCatAuthRewriter; print('OK:', LongCatAuthRewriter)"
```

#### 步骤 3：确认 config.yaml 中 callback 配置正确
```yaml
litellm_settings:
  callbacks: longcat_auth.longcat_auth.longcat_auth_rewriter
```

#### 步骤 4：重启容器并测试
```bash
docker restart litellm
sleep 5
curl -s -w "\nHTTP_CODE: %{http_code}" http://localhost:4000/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-hwj123hwj" \
  -H "Content-Type: application/json" \
  -d '{"model": "longcat", "messages": [{"role": "user", "content": "hi"}], "max_tokens": 10}'
```

期望返回 HTTP 200。

### 备选方案：使用 openai/ 前缀

如果回调方案不行，可以尝试把美团的 model 前缀改为 `openai/`：

```yaml
- model_name: longcat
  litellm_params:
    model: openai/LongCat-Flash-Chat
    api_base: https://api.longcat.chat/v1
    api_key: os.environ/LONGCAT_API_KEY
```

### 注意事项
- 不要使用 `--detailed_debug` 或 `LITELLM_LOG=DEBUG` 启动容器，会导致崩溃
- 修改配置后只需 `docker restart litellm`，不要 `docker rm` 重建容器


---

## 问题 2：DATABASE_URL 指向不存在的数据库

### 症状
```
docker logs litellm
# ERROR: Can't reach database server at `litellm-db:5432`
# Application startup failed. Exiting.
```

### 原因
1. `.env` 文件中包含 `DATABASE_URL=postgresql://litellm:litellm123@litellm-db:5432/litellm`
2. `litellm-db` 是 docker-compose 创建的容器，单独运行 `docker run` 时无法访问
3. Docker 默认 bridge 网络无法解析 `litellm-db` 主机名

### 解决方案
**方案 A：使用 `--network host` 模式（推荐本地部署）**
```bash
docker run -d \
  --name litellm \
  --network host \
  # ... 其他参数
```

**方案 B：创建不带 DATABASE_URL 的 .env 文件**

**方案 C：使用 docker-compose 启动整套服务（服务器部署）**
```bash
docker-compose up -d
```

---

## 问题 3：Callbacks 配置错误

### 症状
```
docker logs litellm
# ImportError: Could not import longcat_auth_rewriter from longcat_auth
# Application startup failed. Exiting.
```

### 原因
`config.yaml` 中 callback 配置格式错误：

```yaml
# 错误配置
litellm_settings:
  callbacks: longcat_auth.longcat_auth_rewriter
```

类名应该是 `LongCatAuthRewriter`，但配置中写的是 `longcat_auth_rewriter`。

### 解决方案
编辑 `~/.litellm/config.yaml`，删除或注释掉 callback 配置行：

```yaml
# 删除或注释掉这行
# callbacks: longcat_auth.longcat_auth_rewriter
```

### ⚠️ 重要提醒
**这个配置可能会被其他工具（如 IDE、git pull 等）还原！**

如果容器启动失败，先检查 `~/.litellm/config.yaml` 是否包含以下错误配置：
```yaml
litellm_settings:
  callbacks: longcat_auth.longcat_auth.longcat_auth_rewriter  # 正确格式
```

---

## 问题 4：Docker Hub 拉取镜像超时

### 症状
```
docker-compose up -d
# Error: failed to resolve reference "docker.io/library/nginx:alpine"
# net/http: TLS handshake timeout
```

### 原因
国内网络访问 Docker Hub 不稳定，拉取镜像超时。

### 解决方案
**方案 A：重试**
```bash
docker-compose up -d
```

**方案 B：使用本地部署（不需要 nginx/certbot）**
```bash
docker run -d \
  --name litellm \
  --network host \
  # ... 本地部署命令
```

**方案 C：配置 Docker 镜像加速器**
编辑 `~/.docker/daemon.json`：
```json
{
  "registry-mirrors": [
    "https://docker.mirrors.ustc.edu.cn",
    "https://hub-mirror.c.163.com"
  ]
}
```
然后重启 Docker：
```bash
sudo systemctl restart docker
```

---

## 正确的本地部署启动命令

```bash
docker stop litellm && docker rm litellm && docker run -d \
  --name litellm \
  --network litellm-net \
  -p 4000:4000 \
  --env-file ~/.litellm/.env \
  -v ~/.litellm/config.yaml:/app/config.yaml \
  -v ~/.litellm/longcat_auth.py:/app/longcat_auth/longcat_auth.py \
  ghcr.io/berriai/litellm:main-latest \
  --config /app/config.yaml
```

### 验证服务
```bash
curl -X POST http://localhost:4000/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-hwj123hwj" \
  -H "Content-Type: application/json" \
  -d '{"model": "glm-sonnet", "messages": [{"role": "user", "content": "Hello"}], "max_tokens": 100}'
```

---

## 快速检查清单

| 检查项 | 命令 |
|--------|------|
| 容器状态 | `docker ps \| grep litellm` |
| 容器日志 | `docker logs litellm --tail 30` |
| 服务响应 | `curl http://localhost:4000/health` |
| 环境变量 | `docker exec litellm env \| grep LITELLM` |
| 检查 callback 配置 | `grep callback ~/.litellm/config.yaml` |

---

## 问题 5：Callback 配置正确但容器仍无法启动

### 症状
配置看起来正确，但容器启动失败。

### 解决方案
确认 `.env` 文件包含必要环境变量：
```bash
# 确认环境变量
docker exec litellm env | grep LITELLM_USE_CHAT_COMPLETIONS
# 应该显示：LITELLM_USE_CHAT_COMPLETIONS_URL_FOR_ANTHROPIC_MESSAGES=true

# 如果没有，删除并重建容器
docker stop litellm && docker rm litellm
docker run -d \
  --name litellm \
  --network litellm-net \
  -p 4000:4000 \
  --env-file ~/.litellm/.env \
  -v ~/.litellm/config.yaml:/app/config.yaml \
  -v ~/.litellm/longcat_auth.py:/app/longcat_auth/longcat_auth.py \
  ghcr.io/berriai/litellm:main-latest \
  --config /app/config.yaml
```
