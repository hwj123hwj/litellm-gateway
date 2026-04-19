# PRD: AI 模型统一网关 (LiteLLM Gateway)

## 背景

当前开发中需要使用多个 AI 模型提供商（智谱 GLM、Anthropic Claude、GitHub Copilot 等），各工具（Claude Code、OpenClaw、自定义脚本）需要分别配置 API key 和 endpoint，管理混乱。

需要搭建一个本地 AI 网关，统一所有模型入口，实现灵活切换和负载均衡。

## 目标

1. 本地部署 LiteLLM 代理服务，统一暴露 OpenAI 兼容的 API 接口
2. 接入多个模型提供商，支持按需切换
3. 所有 API key 集中管理，工具端只需配一个地址
4. 支持自动 fallback（主模型不可用时自动切备用模型）

## 模型提供商

| 提供商 | 模型 | 用途 | 优先级 |
|--------|------|------|--------|
| 智谱 GLM | glm-4-plus, glm-4-flash | 日常编码、便宜任务 | 主力 |
| Anthropic | claude-sonnet-4, claude-haiku | 复杂推理 | 备用 |
| GitHub Copilot | claude-sonnet-4.5, gpt-5 | 额度消耗型 | 补充（谨慎使用） |
| OpenAI | gpt-4o, gpt-4.1-mini | 通用 | 按需 |

## 技术方案

### 技术栈
- **LiteLLM**: Python 库 + 代理模式
- **部署方式**: 本地 Docker 或直接 Python 运行
- **端口**: 4000（默认）

### 配置文件结构

```
~/.litellm/
├── config.yaml          # LiteLLM 主配置（模型列表、provider）
├── .env                 # API keys（不提交 git）
└── docker-compose.yaml  # 可选：Docker 部署
```

### config.yaml 要求

```yaml
model_list:
  # 智谱 GLM
  - model_name: glm
    litellm_params:
      model: openai/glm-4-plus
      api_base: https://open.bigmodel.cn/api/paas/v4
      api_key: os.environ/GLM_API_KEY

  # GLM Flash（轻量任务）
  - model_name: glm-flash
    litellm_params:
      model: openai/glm-4-flash
      api_base: https://open.bigmodel.cn/api/paas/v4
      api_key: os.environ/GLM_API_KEY

  # Anthropic Claude
  - model_name: claude
    litellm_params:
      model: anthropic/claude-sonnet-4-20250514
      api_key: os.environ/ANTHROPIC_API_KEY

  # Anthropic Haiku（快速任务）
  - model_name: claude-haiku
    litellm_params:
      model: anthropic/claude-haiku-4-20250414
      api_key: os.environ/ANTHROPIC_API_KEY

  # GitHub Copilot（灰色地带，谨慎使用）
  - model_name: copilot
    litellm_params:
      model: github_copilot/claude-sonnet-4.5

  # OpenAI
  - model_name: gpt
    litellm_params:
      model: openai/gpt-4o
      api_key: os.environ/OPENAI_API_KEY

  # Fallback 组
  - model_name: coding
    litellm_params:
      model: openai/glm-4-plus
      api_base: https://open.bigmodel.cn/api/paas/v4
      api_key: os.environ/GLM_API_KEY
  - model_name: coding
    litellm_params:
      model: anthropic/claude-sonnet-4-20250514
      api_key: os.environ/ANTHROPIC_API_KEY

router_settings:
  routing: "simple-shuffle"
  num_retries: 2
  timeout: 60
  fallbacks:
    - {"coding": ["coding"]}

general_settings:
  master_key: os.environ/LITELLM_MASTER_KEY  # 网关认证 key
```

### .env 模板

```env
GLM_API_KEY=your_glm_key
ANTHROPIC_API_KEY=your_anthropic_key
OPENAI_API_KEY=your_openai_key
LITELLM_MASTER_KEY=sk-local-gateway-xxx
```

### 启动方式

**方式一：Python 直接运行**
```bash
pip install 'litellm[proxy]'
litellm --config ~/.litellm/config.yaml --port 4000
```

**方式二：Docker**
```bash
docker compose up -d
```

## 客户端配置

### Claude Code
```json
// ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4000",
    "ANTHROPIC_API_KEY": "sk-local-gateway-xxx"
  }
}
```
然后用 `/model` 切换：`/model coding`、`/model claude`、`/model glm`

### OpenClaw
在 `openclaw.json` 中配置模型指向网关。

### 自定义脚本
```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:4000/v1",
    api_key="sk-local-gateway-xxx"
)

response = client.chat.completions.create(
    model="glm",  # 或 claude / gpt / coding
    messages=[{"role": "user", "content": "Hello"}]
)
```

## 非功能需求

1. **安全性**: API key 只存在 `.env` 文件中，不提交 git；`.gitignore` 包含 `.env`
2. **稳定性**: 配置 fallback，单个 provider 挂了自动切下一个
3. **可观测性**: 可选接入 LiteLLM 的 dashboard 查看用量统计
4. **开机自启**: macOS launchd 或 Docker restart policy

## 验收标准

- [ ] `curl http://localhost:4000/v1/models` 返回所有配置的模型列表
- [ ] `curl` 调用每个模型都能正常返回结果
- [ ] Claude Code 通过网关成功调用模型
- [ ] fallback 机制正常工作（关闭某个 provider 后自动切换）
- [ ] `.env` 不在 git 追踪中

## 备注

- GitHub Copilot 代理为灰色地带，仅作补充使用，不当主力
- 智谱 GLM 走官方 API，完全合规
- 后续可考虑将网关部署到服务器上，实现远程访问
