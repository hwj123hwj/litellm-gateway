# 本地 Mac 部署指南

适用于：只在本机使用 Claude Code，不需要远程访问。

## 前置要求

- macOS
- OrbStack（或 Docker Desktop）

## 部署步骤

### 1. 创建配置目录

```bash
mkdir -p ~/.litellm
```

### 2. 创建 .env

```bash
cat > ~/.litellm/.env << 'EOF'
# 智谱 BigModel
GLM_API_KEY=your_glm_key

# 小米 MiMo
MIMO_API_KEY=your_mimo_key

# 美团 LongCat
LONGCAT_API_KEY=your_longcat_key

# 网关认证 key
LITELLM_MASTER_KEY=sk-local-gateway-xxx

# 数据库（PostgreSQL 容器）
DATABASE_URL=postgresql://litellm:litellm123@litellm-db:5432/litellm
EOF
```

把 `your_xxx_key` 替换为实际的 API key。

### 3. 创建 config.yaml

```bash
cat > ~/.litellm/config.yaml << 'EOF'
model_list:
  # ==================== 智谱 BigModel（主力） ====================
  - model_name: glm-haiku
    litellm_params:
      model: anthropic/glm-4.7
      api_base: https://open.bigmodel.cn/api/anthropic
      api_key: os.environ/GLM_API_KEY

  - model_name: glm-sonnet
    litellm_params:
      model: anthropic/glm-5-turbo
      api_base: https://open.bigmodel.cn/api/anthropic
      api_key: os.environ/GLM_API_KEY

  - model_name: glm-opus
    litellm_params:
      model: anthropic/glm-5.1
      api_base: https://open.bigmodel.cn/api/anthropic
      api_key: os.environ/GLM_API_KEY

  # ==================== 小米 MiMo（备用 1） ====================
  - model_name: mimo-haiku
    litellm_params:
      model: anthropic/mimo-v2.5
      api_base: https://token-plan-cn.xiaomimimo.com/anthropic
      api_key: os.environ/MIMO_API_KEY

  - model_name: mimo-sonnet
    litellm_params:
      model: anthropic/mimo-v2.5-pro
      api_base: https://token-plan-cn.xiaomimimo.com/anthropic
      api_key: os.environ/MIMO_API_KEY

  - model_name: mimo-opus
    litellm_params:
      model: anthropic/mimo-v2.5-pro
      api_base: https://token-plan-cn.xiaomimimo.com/anthropic
      api_key: os.environ/MIMO_API_KEY

  # ==================== 美团 LongCat（备用 2） ====================
  - model_name: longcat
    litellm_params:
      model: anthropic/LongCat-Flash-Chat
      api_base: https://api.longcat.chat/anthropic
      api_key: os.environ/LONGCAT_API_KEY

  # ==================== 通用别名（支持 fallback） ====================
  # "coding" 组：智谱优先 → 小米 → 美团
  - model_name: coding
    litellm_params:
      model: anthropic/glm-5-turbo
      api_base: https://open.bigmodel.cn/api/anthropic
      api_key: os.environ/GLM_API_KEY
  - model_name: coding
    litellm_params:
      model: anthropic/mimo-v2.5-pro
      api_base: https://token-plan-cn.xiaomimimo.com/anthropic
      api_key: os.environ/MIMO_API_KEY
  - model_name: coding
    litellm_params:
      model: anthropic/LongCat-Flash-Chat
      api_base: https://api.longcat.chat/anthropic
      api_key: os.environ/LONGCAT_API_KEY

router_settings:
  routing: "simple-shuffle"
  num_retries: 2
  timeout: 120
  fallbacks:
    - {"coding": ["coding"]}
  allowed_fails: 2

general_settings:
  master_key: os.environ/LITELLM_MASTER_KEY

litellm_settings:
  callbacks: longcat_auth.longcat_auth_rewriter
EOF
```

### 4. 创建美团认证回调

美团 LongCat 只接受 `Authorization: Bearer` header，而 LiteLLM 默认发送 `x-api-key`。
需要通过自定义回调来重写认证 header。

```bash
cat > ~/.litellm/longcat_auth.py << 'PYEOF'
import os
import litellm
from litellm.integrations.custom_logger import CustomLogger
from litellm.proxy._types import UserAPIKeyAuth


LONGCAT_MODELS = {"longcat"}


class LongCatAuthRewriter(CustomLogger):
    """Rewrite x-api-key to Authorization: Bearer for longcat provider."""

    async def async_pre_call_hook(
        self,
        user_api_key_dict: UserAPIKeyAuth,
        cache,
        data: dict,
        call_type,
    ):
        model = data.get("model", "")
        if model not in LONGCAT_MODELS:
            return None

        api_key = os.environ.get("LONGCAT_API_KEY", "")
        if not api_key:
            return None

        psh = data.get("provider_specific_header")
        if psh and isinstance(psh, dict):
            extra = dict(psh.get("extra_headers", {}))
            extra["authorization"] = f"Bearer {api_key}"
            extra["x-api-key"] = ""
            psh["extra_headers"] = extra
            data["provider_specific_header"] = psh
        else:
            data["provider_specific_header"] = {
                "extra_headers": {
                    "authorization": f"Bearer {api_key}",
                    "x-api-key": "",
                }
            }

        return data


longcat_auth_rewriter = LongCatAuthRewriter()
PYEOF
```

### 5. 创建 Docker 网络

LiteLLM 需要 PostgreSQL 数据库，两个容器需要在同一网络中通信。

```bash
docker network create litellm-net
```

### 6. 启动 PostgreSQL

```bash
docker run -d \
  --name litellm-db \
  --restart unless-stopped \
  --network litellm-net \
  -e POSTGRES_USER=litellm \
  -e POSTGRES_PASSWORD=litellm123 \
  -e POSTGRES_DB=litellm \
  -v litellm-pgdata:/var/lib/postgresql/data \
  postgres:16-alpine
```

### 7. 启动网关

```bash
docker run -d \
  --name litellm \
  --restart unless-stopped \
  --network litellm-net \
  -p 4000:4000 \
  -v ~/.litellm/config.yaml:/app/config.yaml \
  -v ~/.litellm/.env:/app/.env \
  -v ~/.litellm/longcat_auth.py:/app/longcat_auth.py \
  --env-file ~/.litellm/.env \
  ghcr.io/berriai/litellm:main-latest \
  --config /app/config.yaml --port 4000
```

### 8. 配置 Claude Code

编辑 `~/.claude/settings.json`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4000/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-local-gateway-xxx"
  }
}
```

把 `sk-local-gateway-xxx` 替换为 `.env` 里的 `LITELLM_MASTER_KEY`。

## 验证

```bash
# 测试智谱
curl http://localhost:4000/v1/messages \
  -H "x-api-key: sk-local-gateway-xxx" \
  -H "content-type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"glm-sonnet","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'

# 测试小米
curl http://localhost:4000/v1/messages \
  -H "x-api-key: sk-local-gateway-xxx" \
  -H "content-type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"mimo-sonnet","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'

# 测试美团
curl http://localhost:4000/v1/messages \
  -H "x-api-key: sk-local-gateway-xxx" \
  -H "content-type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"longcat","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'

# 测试 fallback 组
curl http://localhost:4000/v1/messages \
  -H "x-api-key: sk-local-gateway-xxx" \
  -H "content-type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"coding","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'
```

返回有响应就说明配置成功。

## 日常管理

```bash
# 查看日志
docker logs -f litellm

# 重启
docker restart litellm

# 停止
docker stop litellm

# 更新镜像（重建容器）
docker pull ghcr.io/berriai/litellm:main-latest
docker stop litellm && docker rm litellm
# 然后重新执行第 7 步
```

## Fallback 机制

`coding` 模型组包含三个后端（智谱、小米、美团），当请求失败时会自动切换到下一个。

相关配置参数（`config.yaml` 中的 `router_settings`）：

| 参数 | 值 | 说明 |
|------|------|------|
| `routing` | `simple-shuffle` | 从健康的后端中随机选择一个 |
| `num_retries` | `2` | 每个请求最多重试 2 次 |
| `allowed_fails` | `2` | 某个后端连续失败 2 次后，暂时从池中移除 |
| `timeout` | `120` | 单次请求超时 120 秒 |
| `fallbacks` | `[{"coding": ["coding"]}]` | 失败时在同组内其他后端间 fallback |

实际行为：请求 `coding` → 随机选一个后端（比如智谱）→ 超时或报错 → 自动重试 → 再失败 → 选另一个后端（比如小米）→ 成功返回。

## 注意事项

- **端点路径**：必须使用 `/v1/messages`，不要用 `/anthropic/v1/messages`（后者是直通端点，会绕过模型路由）
- **小米模型名**：必须使用小写（如 `mimo-v2.5-pro`），大写会导致模型不存在错误
- **美团认证**：美团只接受 `Authorization: Bearer` header，需要通过 `longcat_auth.py` 回调来重写
- **数据库**：LiteLLM 需要 PostgreSQL，不支持 SQLite
- **回调文件修改**：修改 `longcat_auth.py` 后需要重建容器（restart 不会重新加载 Python 模块）
