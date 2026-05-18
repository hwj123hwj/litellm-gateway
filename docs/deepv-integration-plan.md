# DeepV Server 接入网关规划文档

> 创建时间：2026-05-18
> 更新时间：2026-05-18
> 状态：**已完成** - 直接在 Go Gateway 中实现 DeepV Provider

---

## 一、背景

DeepV Server 是公司内部的 AI 模型聚合服务，支持多种主流模型。将其接入网关可以实现：

1. 统一入口管理所有 AI 模型
2. 与现有提供商（智谱、小米、美团）形成互补
3. 充分利用公司内部资源，降低外部 API 成本

---

## 二、最终实现方案

**采用方案：Go Gateway 直接实现 DeepV Provider**

在 go-gateway 中直接实现 DeepV provider，无需额外运行 bridge 服务。

### 2.1 实现内容

新增文件：`go-gateway/internal/provider/deepv.go`

**核心功能**：
1. **认证**：自动读取 `~/.deepv/jwt-token.json` 获取 access token
2. **Git Headers**：自动获取当前项目的 Git remotes 和 branch
3. **格式转换**：Anthropic 格式 → GenAI 格式
4. **工具调用**：支持 `functionCall` 响应解析

### 2.2 修改的文件

| 文件 | 修改内容 |
|------|----------|
| `internal/provider/deepv.go` | 新增 DeepV Provider 实现 |
| `internal/config/config.go` | 添加 `DEEPV_ENABLED` 和 `DEEPV_WORK_DIR` 配置 |
| `main.go` | 注册 DeepV Provider 和模型链 |
| `internal/provider/router.go` | 添加模型映射 |

### 2.3 配置方式

在 `.env` 中添加：
```bash
DEEPV_ENABLED=true
DEEPV_WORK_DIR=/path/to/your/project  # 用于获取 Git 信息
```

---

## 三、支持的模型

| 模型别名 | 实际模型 | 工具调用 | 状态 |
|----------|----------|----------|------|
| `deepseek-flash` | `deepseek-v4-flash` | ✅ | 已验证 |
| `glm-5` | `glm-5` | ✅ | 已验证 |
| `claude-sonnet-4-6` | `claude-sonnet-4-6` | ✅ | 已验证 |

---

## 四、测试结果

### 4.1 工具调用测试

```bash
# 测试命令
curl -s http://localhost:4001/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-local-gateway-hwj123hwj" \
  -d '{
    "model": "deepseek-flash",
    "max_tokens": 200,
    "tools": [{
      "name": "Bash",
      "description": "Execute bash command",
      "input_schema": {
        "type": "object",
        "properties": {"command": {"type": "string"}},
        "required": ["command"]
      }
    }],
    "messages": [{"role": "user", "content": "What is 2+2? Use Bash tool."}]
  }'
```

**结果**：
```json
{
  "type": "tool_use",
  "name": "Bash",
  "input": {"command": "echo $((2+2))"}
}
```

### 4.2 三个模型测试结果

| 模型 | 工具调用结果 |
|------|--------------|
| `deepseek-flash` | ✅ `Bash: echo $((2+2))` |
| `glm-5` | ✅ `Bash: echo $((2 + 2))` |
| `claude-sonnet-4-6` | ✅ `Bash: echo $((2+2))` |

---

## 五、关键技术点

### 5.1 GenAI 格式转换

**请求格式**：
```json
{
  "model": "deepseek-v4-flash",
  "contents": [{"role": "user", "parts": [{"text": "..."}]}],
  "config": {
    "maxOutputTokens": 200,
    "tools": [{
      "functionDeclarations": [{
        "name": "Bash",
        "description": "...",
        "parameters": {...}
      }]
    }]
  }
}
```

**响应格式**：
```json
{
  "candidates": [{
    "content": {
      "parts": [{
        "functionCall": {
          "name": "Bash",
          "args": {"command": "echo $((2+2))"}
        }
      }]
    }
  }]
}
```

### 5.2 Git Headers

自动获取当前项目的 Git 信息并添加到请求头：
- `X-Git-Remotes`: `{"origin":"https://gitlab.liebaopay.com/..."}`
- `X-Git-Branch`: `main`

---

## 六、与 Bridge 方案对比

| 对比项 | Bridge 方案 | 直接实现 |
|--------|-------------|----------|
| 架构复杂度 | 高（需要额外服务） | 低（单服务） |
| 延迟 | +10-50ms | 无额外延迟 |
| 运维成本 | 高 | 低 |
| 代码复用 | 复用 TypeScript 代码 | Go 独立实现 |
| 功能完整性 | 完整 | 完整 |

**结论**：直接在 Go Gateway 中实现是更优的方案。

---

## 七、后续优化

- [ ] 实现流式响应（SSE）
- [ ] 添加 Token 自动刷新
- [ ] 支持多轮对话的 tool_result 处理
- [ ] 添加健康检查和监控

---

## 八、参考

- DeepV Server API: `https://api-code.deepvlab.ai/v1/chat/messages`
- GenAI 格式文档: `@google/genai` TypeScript 类型定义
