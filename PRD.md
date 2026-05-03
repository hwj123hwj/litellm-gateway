# PRD: Claude Code 多模型网关

## 背景

目前使用多个国产 AI 模型提供商（智谱、小米、美团），它们都提供了 **Anthropic 兼容 API**，可以直接接入 Claude Code。

**痛点**：Claude Code 同一时间只能配置一个提供商，导致其他家的模型和额度闲置。目前主力用智谱，小米和美团的资源基本浪费。

## 目标

搭建一个网关服务，让 Claude Code 通过一个入口访问所有提供商：

1. 智谱主力，自动 fallback 到小米/美团
2. 手动 `/model` 切换到任意提供商
3. 充分利用所有已有资源
4. 支持本地部署和服务器部署两种方式

## 模型提供商

所有提供商均暴露 **Anthropic 兼容 API**（`/v1/messages` 端点）。

| 提供商 | API Base | 模型 | 角色 |
|--------|----------|------|------|
| 智谱 BigModel | `https://open.bigmodel.cn/api/anthropic` | glm-4.7, glm-5-turbo, glm-5.1 | 主力（优先） |
| 小米 MiMo | `https://token-plan-cn.xiaomimimo.com/anthropic` | mimo-v2.5, mimo-v2.5-pro | 备用 1 |
| 美团 LongCat | `https://api.longcat.chat/anthropic` | LongCat-Flash-Chat | 备用 2 |

### 模型分级映射

Claude Code 内部按能力分三个层级，网关为每个层级提供模型映射：

| Claude Code 层级 | 环境变量 | 智谱 | 小米 | 美团 |
|------------------|----------|------|------|------|
| Haiku（快速） | `ANTHROPIC_DEFAULT_HAIKU_MODEL` | glm-4.7 | mimo-v2.5 | LongCat-Flash-Chat |
| Sonnet（主力） | `ANTHROPIC_DEFAULT_SONNET_MODEL` | glm-5-turbo | mimo-v2.5-pro | LongCat-Flash-Chat |
| Opus（强力） | `ANTHROPIC_DEFAULT_OPUS_MODEL` | glm-5.1 | mimo-v2.5-pro | LongCat-Flash-Chat |

## 架构

```
Claude Code ──(Anthropic API)──▶ 网关
                                   ├── 智谱 (Anthropic API)
                                   ├── 小米 (Anthropic API) ── fallback
                                   └── 美团 (Anthropic API) ── fallback
```

所有上下游都是 Anthropic API 格式，网关只做路由和转发。

### 部署方式

| 方式 | 适用场景 | 详细文档 |
|------|----------|----------|
| 本地 Docker | 仅本机使用，最简单 | [docs/local-deploy.md](docs/local-deploy.md) |
| 服务器 + Nginx + HTTPS | 多地点、多设备使用 | [docs/server-deploy.md](docs/server-deploy.md) |

## Claude Code 客户端配置

### settings.json

```json
// ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4000/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-your-gateway-key"
  }
}
```

服务器部署时将 `ANTHROPIC_BASE_URL` 改为 `https://your-domain.com/v1`。

### 模型切换

在 Claude Code 中通过 `/model` 命令手动切换：

| 命令 | 效果 | 说明 |
|------|------|------|
| `/model coding` | 自动 fallback 组 | 智谱优先，挂了切小米→美团 |
| `/model glm-haiku` | 智谱 glm-4.7 | 快速任务 |
| `/model glm-sonnet` | 智谱 glm-5-turbo | 日常编码（推荐） |
| `/model glm-opus` | 智谱 glm-5.1 | 复杂推理 |
| `/model mimo-haiku` | 小米 mimo-v2.5 | 小米快速任务 |
| `/model mimo-sonnet` | 小米 mimo-v2.5-pro | 小米主力 |
| `/model mimo-opus` | 小米 mimo-v2.5-pro | 小米强力 |
| `/model longcat` | 美团 LongCat-Flash-Chat | 手动切美团 |

## 验收标准

- [ ] 网关启动正常，Claude Code 通过网关完成一次完整的编码任务
- [ ] `coding` 组 fallback 正常（关闭智谱后自动切小米/美团）
- [ ] `/model glm-sonnet`、`/model mimo-sonnet`、`/model longcat` 手动切换正常
- [ ] `.env` 不在 git 追踪中
- [ ] 服务器部署时：HTTPS 正常、无认证 key 的请求被拒绝
