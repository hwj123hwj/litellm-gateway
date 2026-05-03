# Claude Code 多模型网关 - 技术实现指南

## 🤖 面向 AI 助手的使用说明

本文件专门为 AI 助手（Claude Code）编写，提供项目的技术实现细节、配置管理和扩展指南。

## 📁 项目结构

```
litellm-gateway/
├── .claude/                 # 本文件所在目录
├── docs/                   # 用户文档
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
└── README.md              # 用户导向文档
```

## ⚙️ 核心配置文件

### 1. LiteLLM 配置 (~/.litellm/config.yaml)

**职责**: 模型路由、fallback 机制、认证配置

**关键配置项**:
```yaml
model_list:
  # 智谱 BigModel
  - model_name: glm-sonnet
    litellm_params:
      model: anthropic/glm-5-turbo
      api_base: https://open.bigmodel.cn/api/anthropic
      api_key: os.environ/GLM_API_KEY

router_settings:
  routing: "simple-shuffle"
  num_retries: 2
  timeout: 120
  fallbacks:
    - {"coding": ["coding"]}
  allowed_fails: 2

litellm_settings:
  callbacks: longcat_auth.longcat_auth_rewriter
```

### 2. 美团认证回调 (~/.litellm/longcat_auth.py)

**问题**: 美团 API 只接受 `Authorization: Bearer`，LiteLLM 默认发送 `x-api-key`

**解决方案**: 使用 `async_pre_call_hook` 拦截并重写 headers

```python
from litellm import CustomLogger

class CustomAuth(CustomLogger):
    async def async_pre_call_hook(
        self, kwargs, response, call_type
    ):
        if kwargs.get("model") == "longcat":
            api_key = kwargs.get("api_key")
            extra_headers = kwargs.get("extra_headers", {})
            extra_headers["Authorization"] = f"Bearer {api_key}"
            
            if "x-api-key" in extra_headers:
                del extra_headers["x-api-key"]
                
            kwargs["extra_headers"] = extra_headers
        return kwargs
```

### 3. Claude Code 配置 (~/.claude/settings.json)

**关键配置**:
```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4000",
    "ANTHROPIC_AUTH_TOKEN": "your-gateway-token-here",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-sonnet"
  }
}
```

## 🔧 部署管理

### 本地部署流程

1. **环境准备**
   ```bash
   # 创建配置目录
   mkdir -p ~/.litellm
   
   # 复制配置文件
   cp litellm/config.yaml ~/.litellm/
   cp litellm/longcat_auth.py ~/.litellm/
   ```

2. **环境变量配置**
   ```bash
   # 创建 .env 文件
   GLM_API_KEY="your-glm-key"
   MIMO_API_KEY="your-mimo-key"
   LONGCAT_API_KEY="your-longcat-key"
   LITELLM_MASTER_KEY="your-secure-gateway-token"
   ```

3. **启动服务**
   ```bash
   docker run -d \
     --name litellm-proxy \
     -p 4000:4000 \
     -v ~/.litellm/config.yaml:/app/config.yaml \
     -v ~/.litellm/longcat_auth.py:/app/longcat_auth/longcat_auth.py \
     -e LITELLM_MASTER_KEY=$LITELLM_MASTER_KEY \
     -e GLM_API_KEY=$GLM_API_KEY \
     -e MIMO_API_KEY=$MIMO_API_KEY \
     -e LONGCAT_API_KEY=$LONGCAT_API_KEY \
     ghcr.io/berriai/litellm:main-latest \
     --config /app/config.yaml
   ```

### 服务器部署流程

1. **使用 docker-compose**
   ```bash
   # 配置域名
   DOMAIN="your-domain.com"
   
   # 启动服务栈
   docker-compose up -d
   ```

2. **Nginx 配置要点**
   - HTTPS 强制跳转
   - 速率限制 (100 req/min)
   - 反向代理到 LiteLLM

## 🧪 测试验证

### 测试网关连接
```bash
curl -X POST http://localhost:4000/v1/messages \
  -H "Authorization: Bearer $LITELLM_MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-sonnet",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 100
  }'
```

### 测试 fallback 机制
1. 正常情况：智谱响应
2. 关闭智谱 API：自动切换到小米
3. 关闭小米 API：自动切换到美团

## 🚀 扩展新模型指南

### 标准接入流程

1. **API 兼容性检查**
   - ✅ 支持 Anthropic API 格式 (`/v1/messages`)
   - ✅ 认证方式 (x-api-key vs Authorization: Bearer)
   - ✅ 模型名称和参数支持

2. **配置添加**
   ```yaml
   # ~/.litellm/config.yaml
   - model_name: new-provider-sonnet
     litellm_params:
       model: anthropic/new-provider-model
       api_base: https://api.new-provider.com/anthropic
       api_key: os.environ/NEW_PROVIDER_API_KEY
   ```

3. **认证兼容性处理**
   - 标准 x-api-key：直接配置即可
   - Bearer token：需要添加认证回调
   - 自定义 header：修改 `longcat_auth.py`

4. **测试验证**
   - 单元测试：验证单个模型连接
   - 集成测试：验证网关路由
   - Fallback 测试：验证容错机制

### 美团接入经验总结

**遇到的问题**:
- ❌ LiteLLM 默认: `x-api-key` header
- ❌ 美团要求: `Authorization: Bearer <token>`
- ❌ 结果: `missing_api_key` 错误

**解决方案**:
- ✅ 使用 `CustomLogger.async_pre_call_hook`
- ✅ 拦截请求，重写 headers
- ✅ 保持认证逻辑模块化

**代码模式**:
```python
if kwargs["model"] == "longcat":
    extra_headers["Authorization"] = f"Bearer {api_key}"
    if "x-api-key" in extra_headers:
        del extra_headers["x-api-key"]
```

### 通用扩展建议

1. **配置管理**
   - 每个提供商独立配置块
   - 使用有意义的模型别名
   - 保持配置文件格式统一

2. **错误处理**
   - 添加详细的错误日志
   - 实现优雅的 fallback 逻辑
   - 监控模型可用性

3. **性能优化**
   - 设置合适的超时时间
   - 实现连接池复用
   - 监控响应时间和错误率

## 🔍 调试技巧

### 常见错误诊断

| 错误 | 原因 | 解决方案 |
|------|------|----------|
| `model not found` | 模型名称错误或大小写问题 | 检查 config.yaml 中的 model_name |
| `missing_api_key` | 认证 header 不兼容 | 检查认证回调配置 |
| `404 not found` | API base URL 错误 | 验证 api_base 地址 |
| `timeout` | 网络或模型响应慢 | 调整 timeout 设置 |

### 日志查看
```bash
# LiteLLM 日志
docker logs litellm_proxy

# Nginx 访问日志
tail -f /var/log/nginx/access.log

# 错误日志
tail -f /var/log/nginx/error.log
```

## 🔒 安全最佳实践

### 敏感信息管理
- ✅ 永远不要提交 `.env` 文件
- ✅ 使用 `.env.example` 作为模板
- ✅ 定期轮换 API keys
- ✅ 使用强密码作为网关认证令牌

### 网络安全
- ✅ 服务器部署强制 HTTPS
- ✅ 启用 Nginx 速率限制
- ✅ 防火墙限制访问 IP
- ✅ 定期更新证书

## 📊 项目状态

✅ **已完成**:
- [x] 三家模型接入和测试
- [x] Fallback 机制实现
- [x] 手动切换功能
- [x] 本地部署方案
- [x] 服务器部署方案
- [x] 认证兼容性处理
- [x] 完整文档编写
- [x] 扩展指南

🎯 **项目状态**: 生产就绪，可直接提交开源

## 💡 AI 助手使用提示

当处理此项目时，请记住：

1. **配置优先级**: 环境变量 > config.yaml > 默认值
2. **认证流程**: Claude Code → 网关认证 → 模型路由 → 提供商认证
3. **错误排查**: 先检查配置，再检查网络，最后检查认证
4. **扩展原则**: 保持向后兼容，模块化设计，详细文档

如需修改任何配置，请确保同步更新相关文档和测试用例。