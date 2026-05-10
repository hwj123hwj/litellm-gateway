# EasyClaw 模型接入文档

## 问题描述

EasyClaw 的 claude-sonnet-4-6 模型只支持 OpenAI 格式的 `/v1/chat/completions` 端点，而 Claude Code 发送的是 Anthropic 格式的 `/v1/messages` 请求。

LiteLLM 虽然支持 Anthropic Messages 端点，但默认会将请求路由到 OpenAI 的 `/v1/responses` 端点，导致 404 错误。

## 解决方案

### 1. 添加环境变量

在 `litellm/.env` 文件中添加：

```bash
# 强制 LiteLLM 对 Anthropic Messages 请求使用 /v1/chat/completions 而不是 Responses API
LITELLM_USE_CHAT_COMPLETIONS_URL_FOR_ANTHROPIC_MESSAGES=true
```

### 2. 配置文件

在 `litellm/config.yaml` 中配置模型：

```yaml
- model_name: claude-sonnet-4-6
  litellm_params:
    model: openai/claude-sonnet-4-6
    api_base: https://api.easyclaw.work
    api_key: os.environ/EASYCLAW_API_KEY
```

**注意**：`api_base` 只需要写到域名，不要带 `/v1/chat/completions` 后缀，LiteLLM 会自动处理。

### 3. 启动容器

必须使用 `--env-file` 参数加载环境变量：

```bash
docker stop litellm && docker rm litellm

docker run -d \
  --name litellm \
  --network litellm-net \
  -p 4000:4000 \
  --env-file /Users/weijian/Desktop/develop/test/litellm-gateway/litellm/.env \
  -v /Users/weijian/Desktop/develop/test/litellm-gateway/litellm/config.yaml:/app/config.yaml \
  -v /Users/weijian/Desktop/develop/test/litellm-gateway/litellm/longcat_auth.py:/app/longcat_auth/longcat_auth.py \
  ghcr.io/berriai/litellm:main-latest \
  --config /app/config.yaml
```

## 验证方法

```bash
curl -X POST http://localhost:4000/v1/messages \
  -H "Authorization: Bearer sk-local-gateway-hwj123hwj" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "messages": [{"role": "user", "content": "Hi"}],
    "max_tokens": 50
  }'
```

成功响应示例：
```json
{
  "id": "chatcmpl-xxx",
  "type": "message",
  "role": "assistant",
  "model": "claude-sonnet-4-6",
  "content": [{"type": "text", "text": "Hi there!..."}],
  "stop_reason": "end_turn"
}
```

## 原理说明

LiteLLM 内置了 Anthropic Messages ↔ OpenAI Chat Completions 的格式转换功能：

1. Claude Code 发送 Anthropic 格式的 `/v1/messages` 请求
2. LiteLLM 的 `/v1/messages` 端点接收请求
3. 通过 `LITELLM_USE_CHAT_COMPLETIONS_URL_FOR_ANTHROPIC_MESSAGES=true` 强制转换
4. LiteLLM 将请求转换为 OpenAI 格式，调用 easyclaw 的 `/v1/chat/completions`
5. easyclaw 返回 OpenAI 格式响应
6. LiteLLM 将响应转换回 Anthropic 格式返回给 Claude Code

## 关键要点

1. **环境变量必须设置**：`LITELLM_USE_CHAT_COMPLETIONS_URL_FOR_ANTHROPIC_MESSAGES=true`
2. **必须使用 --env-file**：否则环境变量不会加载到容器中
3. **api_base 不要带后缀**：保持 `https://api.easyclaw.work` 即可
4. **model 前缀用 openai/**：告诉 LiteLLM 这是 OpenAI 格式的模型