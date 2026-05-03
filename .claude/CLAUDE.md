# Claude Code AI 模型网关项目指南

## 项目概述

这是一个基于 LiteLLM Proxy 的 AI 模型网关，让 Claude Code 能够同时接入智谱、小米、美团三家国产 AI 模型。项目支持自动 fallback 机制和手动模型切换。

## 核心功能

- **智谱主力模型**：默认使用智谱 BigModel，自动 fallback 到小米 MiMo 和美团 LongCat
- **手动切换**：支持 `/model` 命令在 Claude Code 中切换模型
- **部署方式**：支持本地部署（仅本机）和服务器部署（多设备访问）
- **API 兼容**：完全兼容 Anthropic API，无需修改 Claude Code

## 支持模型

| 命令 | 效果 |
|------|------|
| `/model coding` | fallback 组（智谱优先 → 小米 → 美团）|
| `/model glm-sonnet` | 智谱主力（日常推荐）|
| `/model mimo-sonnet` | 小米主力 |
| `/model longcat` | 美团 LongCat |

## 项目结构

```
litellm-gateway/
├── .claude/                 # Claude 配置文件
├── docs/                   # 部署文档
│   ├── local-deploy.md    # 本地部署指南
│   └── server-deploy.md   # 服务器部署指南
├── docker-compose.yaml     # 服务器部署编排
├── nginx/                  # Nginx 配置
├── scripts/                # 部署脚本
│   └── init.sh           # 一键部署脚本
├── litellm/               # LiteLLM 配置
│   ├── config.yaml       # 模型路由配置
│   ├── .env.example      # 环境变量模板
│   └── longcat_auth.py   # 美团认证回调
└── README.md              # 项目说明
```

## 配置文件位置

- **LiteLLM 配置**: `~/.litellm/config.yaml`
- **认证回调**: `~/.litellm/longcat_auth.py`
- **环境变量**: `~/.litellm/.env`
- **Claude Code 配置**: `~/.claude/settings.json`

## 关键配置说明

### 1. 模型路由配置 (config.yaml)
- 定义三家提供商的模型映射
- 设置 fallback 机制（simple-shuffle 路由）
- 配置认证回调处理美团 API

### 2. 美团认证重写 (longcat_auth.py)
- 使用 `async_pre_call_hook` 拦截请求
- 将 `x-api-key` 转换为 `Authorization: Bearer`
- 解决美团 API 认证兼容性问题

### 3. Claude Code 配置 (settings.json)
- `ANTHROPIC_BASE_URL`: 指向网关地址
- `ANTHROPIC_AUTH_TOKEN`: 认证令牌
- 默认模型映射配置

## 部署方式

### 本地部署
1. 安装 OrbStack 或 Docker Desktop
2. 配置环境变量和认证文件
3. 启动 LiteLLM 服务
4. 配置 Claude Code 指向本地网关

### 服务器部署
1. 准备 Ubuntu 服务器和域名
2. 使用 docker-compose 部署完整服务栈
3. 配置 Nginx + HTTPS + Certbot
4. 客户端配置指向服务器

## 故障排除

### 常见错误
- **missing_api_key**: 美团认证 header 不兼容，检查 longcat_auth.py
- **model not found**: 模型名称大小写问题，确保使用小写
- **404 error**: 检查 ANTHROPIC_BASE_URL 是否包含 /v1

### 调试方法
1. 检查 LiteLLM 日志：`docker logs litellm_proxy`
2. 验证 API keys 有效性
3. 测试单个模型连接
4. 检查网络连接和防火墙

## 安全注意事项

- 永远不要提交 `.env` 文件
- 使用环境变量或 `.env.example` 模板
- 定期轮换 API keys
- 服务器部署时启用 HTTPS

## 敏感信息

以下文件包含敏感信息，已添加到 `.gitignore`：
- `~/.litellm/.env` - API keys 和数据库连接
- `certbot/conf/` - SSL 证书配置
- `certbot/www/` - Certbot 验证文件

## 扩展新模型指南

### 接入新提供商的标准流程

1. **调研 API 兼容性**
   - 确认是否支持 Anthropic API 格式
   - 检查认证方式（x-api-key vs Authorization: Bearer）
   - 验证模型名称和参数格式

2. **配置模型映射**
   在 `~/.litellm/config.yaml` 的 `model_list` 中添加：
   ```yaml
   - model_name: new-provider-sonnet
     litellm_params:
       model: anthropic/new-provider-model-name
       api_base: https://api.new-provider.com/anthropic
       api_key: os.environ/NEW_PROVIDER_API_KEY
   ```

3. **处理认证兼容性**
   如果新提供商需要特殊的认证 header：
   - 检查是否需要类似美团的 header 重写
   - 在 `longcat_auth.py` 中添加新的处理逻辑
   - 或使用独立的认证回调文件

4. **测试连接**
   ```bash
   curl -X POST http://localhost:4000/v1/messages \
     -H "Authorization: Bearer $LITELLM_MASTER_KEY" \
     -H "Content-Type: application/json" \
     -d '{
       "model": "new-provider-sonnet",
       "messages": [{"role": "user", "content": "Hello"}],
       "max_tokens": 100
     }'
   ```

### 美团模型接入经验总结

美团 LongCat 的接入确实遇到了一些特殊挑战：

**问题分析：**
- LiteLLM 默认发送 `x-api-key` header
- 美团 API 只接受 `Authorization: Bearer <token>`
- 直接配置会导致 `missing_api_key` 错误

**解决方案：**
1. 使用 `CustomLogger.async_pre_call_hook` 拦截请求
2. 在请求发送到美团前重写 headers
3. 将 `x-api-key` 转换为 `Authorization: Bearer`

**代码实现：**
```python
async def async_pre_call_hook(
    kwargs,                # 包含所有请求参数
    response,             # 响应对象
    call_type             # 调用类型
):
    if kwargs["model"] == "longcat":
        # 获取原始 API key
        api_key = kwargs.get("api_key")
        
        # 重写 headers
        extra_headers = kwargs.get("extra_headers", {})
        extra_headers["Authorization"] = f"Bearer {api_key}"
        
        # 移除 x-api-key
        if "x-api-key" in extra_headers:
            del extra_headers["x-api-key"]
            
        kwargs["extra_headers"] = extra_headers
```

**经验教训：**
- 先测试直接 API 调用确认兼容性
- 查看 LiteLLM 文档了解认证机制
- 利用 `async_pre_call_hook` 处理特殊认证需求
- 保持认证逻辑的模块化，便于维护

### 通用扩展建议

1. **配置分离**：将每个提供商的配置独立管理
2. **错误处理**：添加适当的错误日志和 fallback 逻辑
3. **性能监控**：监控新模型的响应时间和成功率
4. **文档更新**：及时更新 README 和部署文档
5. **测试覆盖**：为新模型添加完整的测试用例

## 项目状态

✅ 已完成功能：
- 三家模型接入和测试
- Fallback 机制实现
- 手动切换功能
- 本地和服务器部署方案
- 完整文档编写
- 扩展指南编写

项目已准备就绪，可以直接提交开源。新模型接入现在有了清晰的指导流程。