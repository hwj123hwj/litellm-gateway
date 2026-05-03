# Claude Code 多模型网关

一个 [LiteLLM Proxy](https://litellm.ai) 网关，让 Claude Code 同时接入智谱、小米、美团三家国产 AI 模型。支持 fallback 和手动切换，充分利用所有已有资源。

## 功能

- 智谱主力，自动 fallback 到小米/美团
- 手动 `/model` 切换到任意提供商
- 本地部署（仅本机）和服务器部署（多设备）两种方式
- 全部 Anthropic 兼容 API，无需改动 Claude Code

## 快速开始

### 本地部署

1. 安装 [OrbStack](https://orbstack.dev)（或 Docker Desktop）
2. 按 [docs/local-deploy.md](docs/local-deploy.md) 部署
3. 改 `~/.claude/settings.json` 指向网关

### 服务器部署

1. 准备 Ubuntu 服务器、域名、DNS 解析
2. 按 [docs/server-deploy.md](docs/server-deploy.md) 部署
3. 客户端改 `~/.claude/settings.json` 指向服务器

## 模型切换

在 Claude Code 对话框中输入：

| 命令 | 效果 |
|------|------|
| `/model coding` | fallback 组（智谱优先，挂了切小米→美团） |
| `/model glm-sonnet` | 智谱主力（日常推荐） |
| `/model mimo-sonnet` | 小米主力 |
| `/model longcat` | 美团 |

## 架构

```
Claude Code ──(Anthropic API)──▶ 网关
                                   ├── 智谱 (Anthropic API)
                                   ├── 小米 (Anthropic API) ── fallback
                                   └── 美团 (Anthropic API) ── fallback
```

## 许可证

MIT
