# Claude Code 多模型网关

一个 [LiteLLM Proxy](https://litellm.ai) 网关，让 Claude Code 同时接入智谱、小米、美团三家国产 AI 模型。支持自动 fallback 和手动切换，充分利用所有已有资源。

## 🎯 解决的问题

**痛点**：Claude Code 一次只能用一个 AI 模型提供商，其他家的资源闲置浪费。

**解决方案**：通过网关统一管理所有模型，智能路由 + 自动容错 + 手动切换。

## ✨ 核心功能

- **智能路由**：智谱主力，自动 fallback 到小米/美团
- **手动切换**：支持 `/model` 命令自由切换任意提供商
- **双部署模式**：本地部署（仅本机）和服务器部署（多设备）
- **完全兼容**：100% Anthropic API 兼容，无需修改 Claude Code
- **资源利用**：充分利用所有已有 API 额度

## 🚀 快速开始

### 本地部署（推荐初学者）

1. 安装 [OrbStack](https://orbstack.dev) 或 Docker Desktop
2. 按 [本地部署指南](docs/local-deploy.md) 配置
3. 修改 `~/.claude/settings.json` 指向网关

### 服务器部署（多设备共享）

1. 准备 Ubuntu 服务器 + 域名 + DNS 解析
2. 按 [服务器部署指南](docs/server-deploy.md) 配置
3. 所有客户端修改设置指向服务器

## 🔄 模型切换

在 Claude Code 对话框中输入以下命令：

| 命令 | 效果 | 推荐场景 |
|------|------|----------|
| `/model coding` | 自动 fallback（智谱→小米→美团） | 日常使用（推荐） |
| `/model glm-sonnet` | 智谱主力模型 | 高质量代码生成 |
| `/model mimo-sonnet` | 小米主力模型 | 备选主力 |
| `/model longcat` | 美团 LongCat | 特殊需求 |

## 🏗️ 架构概览

```
Claude Code ──(Anthropic API)──▶ 网关 ──┬── 智谱 (主力)
                                         ├── 小米 (备用)
                                         └── 美团 (备用)
```

## 📋 支持的模型

| 提供商 | Haiku (快速) | Sonnet (主力) | Opus (强力) |
|--------|--------------|---------------|-------------|
| 智谱 BigModel | glm-haiku | glm-sonnet | glm-opus |
| 小米 MiMo | mimo-haiku | mimo-sonnet | mimo-opus |
| 美团 LongCat | longcat | longcat | longcat |

## 🔧 配置说明

### 本地使用
- 网关地址：`http://localhost:4000`
- 认证令牌：自定义安全令牌

### 服务器使用
- 网关地址：`https://your-domain.com`
- 自动 HTTPS + 域名配置

## 📖 详细文档

- [本地部署指南](docs/local-deploy.md) - 一步一步本地搭建教程
- [服务器部署指南](docs/server-deploy.md) - 生产环境部署方案
- [技术实现细节](.claude/CLAUDE.md) - 开发者技术文档
- [产品需求文档](PRD.md) - 产品功能规格说明

## 🛡️ 安全特性

- API keys 本地存储，不提交到代码库
- 支持 HTTPS 加密传输（服务器部署）
- 认证令牌保护网关访问
- 完整的敏感信息保护

## 📄 许可证

MIT License

## 🤝 贡献指南

欢迎贡献代码和文档！请确保：

1. 不提交包含敏感信息的配置文件
2. 更新相关文档
3. 测试所有功能正常工作