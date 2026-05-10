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

# EasyClaw
EASYCLAW_API_KEY=your_easyclaw_key

# 网关认证 key
LITELLM_MASTER_KEY=sk-local-gateway-xxx

# 数据库（PostgreSQL 容器）
DATABASE_URL=postgresql://litellm:litellm123@litellm-db:5432/litellm

# 强制 LiteLLM 对 Anthropic Messages 请求使用 /v1/chat/completions
LITELLM_USE_CHAT_COMPLETIONS_URL_FOR_ANTHROPIC_MESSAGES=true
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
  - model_name: longcat-sonnet
    litellm_params:
      model: anthropic/LongCat-Flash-Chat
      api_base: https://api.longcat.chat/anthropic
      api_key: os.environ/LONGCAT_API_KEY
  - model_name: longcat-opus
    litellm_params:
      model: anthropic/LongCat-2.0-Preview
      api_base: https://api.longcat.chat/anthropic
      api_key: os.environ/LONGCAT_API_KEY

  # ==================== EasyClaw（OpenAI 格式，需要格式转换） ====================
  - model_name: claude-sonnet-4-6
    litellm_params:
      model: openai/claude-sonnet-4-6
      api_base: https://api.easyclaw.work
      api_key: os.environ/EASYCLAW_API_KEY
      drop_params: true

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
  callbacks: longcat_auth.longcat_auth.longcat_auth_rewriter
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


LONGCAT_MODELS = {"longcat-sonnet", "longcat-opus"}


class LongCatAuthRewriter(CustomLogger):
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

        extra_headers = data.get("extra_headers", {})
        extra_headers["Authorization"] = f"Bearer {api_key}"
        if "x-api-key" in extra_headers:
            del extra_headers["x-api-key"]
        data["extra_headers"] = extra_headers
        return data


longcat_auth_rewriter = LongCatAuthRewriter()
PYEOF
```

### 5. 一键启动网关

```bash
./scripts/start-local.sh
```

脚本会自动：
- 检查/启动 Docker
- 检查配置文件
- 启动 PostgreSQL 数据库（如已存在）
- 启动 LiteLLM 网关

启动成功后显示：
```
=========================================
  网关已启动
=========================================

  访问地址: http://localhost:4000
  认证 Token: sk-local-gateway-xxx
```

### 6. 配置 Claude Code

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
curl -X POST http://localhost:4000/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"glm-sonnet","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'

# 测试小米
curl -X POST http://localhost:4000/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"mimo-sonnet","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'

# 测试美团
curl -X POST http://localhost:4000/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"longcat-sonnet","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'

# 测试 EasyClaw（OpenAI 格式）
curl -X POST http://localhost:4000/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-6","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'

# 测试 fallback 组
curl -X POST http://localhost:4000/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"coding","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'
```

返回有响应就说明配置成功。

## 日常管理

```bash
# 一键启动（推荐）
./scripts/start-local.sh

# 查看日志
docker logs -f litellm

# 重启网关
docker restart litellm

# 停止网关
docker stop litellm

# 更新镜像
docker pull ghcr.io/berriai/litellm:main-latest
docker stop litellm && docker rm litellm
./scripts/start-local.sh
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
