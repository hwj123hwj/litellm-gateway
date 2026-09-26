# Agent 长期记忆层设计（v1）

> 状态：v1 已实现（存储 + 作用域路由 + 治理 API）；注入钩子与自动提取为后续阶段。
> 关联：PRD.md R7（归档与知识库导出）、AGENTS.md 数据红线。

## 1. 定位与分工

网关在记忆体系里承担**底座**角色，与既有分工对齐：

| 层 | 职责 |
|---|---|
| 网关（本仓库） | 记忆存储、作用域路由、命中观测、治理 API、（后续）请求注入 |
| agent-lessons | 从归档流编译候选知识（后续接入：编译产物以 candidate 落回本层） |
| hwj-wiki / .llm-wiki | 长文档与代码事实的检索，与本层互补不重叠 |

设计原则：

- **命题化**：一条记忆 = 一条可检验的陈述（带来源与时间），不是聊天摘要；
- **作用域强制**：每条记忆必须属于 global / client / project 之一，杜绝跨域污染；
- **agent 起草、人拍板**：`/v1/memory` 写入一律进 `candidate`，`active` 必须经 admin 确认；
- **密钥红线**：写入前做模式守门，疑似密钥/凭据直接 422 拒绝（Token/密钥永不落库）。

## 2. 数据模型

表 `agent_memories`（SQLite，与 metrics/archives 同文件、物理隔离的表）：

| 字段 | 说明 |
|---|---|
| scope_type | `global` \| `client` \| `project` |
| scope_key | global 为空；client 为客户端名；project 为 `host:owner/repo` |
| statement | 命题正文（≤500 字符，写入前守门过滤） |
| status | `candidate` → `active` ⇄ `retired`（dead 用 delete） |
| source | `manual` \| `agent:<client>` \| `api` \|（规划）`archive:<request_id>`、`git:<sha>` |
| confidence / hit_count / last_hit_at | 检索排序与遗忘依据 |

`UNIQUE(scope_type, scope_key, statement)`：重复命题刷新既有行（吸收 agent 重试），不重复入库。

### 作用域路由

`LookupFor(client, project)` 合并三环：project ⊕ client ⊕ global，越窄越靠前（与 AGENTS.md 的目录作用域同构），命中自动累计 `hit_count` —— 这是后续"遗忘"（长期零命中降权/清理）的数据基础。

## 3. API（v1 已实现）

客户端（API Key 鉴权）：

| 端点 | 说明 |
|---|---|
| `GET /v1/memory?client=&project=&limit=` | 按作用域检索 active 记忆，响应含 `block`（渲染好的 `<agent-memory>` 注入块，wrapper 可直接用） |
| `POST /v1/memory` | 起草候选：`{statement, scope_type, scope_key, client}` → `candidate` |

管理端（Admin Token 鉴权）：

| 端点 | 说明 |
|---|---|
| `GET /admin/memories?status=&scope_type=&scope_key=` | 列表（分页） |
| `POST /admin/memories` | 人工直接创建（`active`） |
| `POST /admin/memories/:id/confirm` | candidate → active（人拍板） |
| `POST /admin/memories/:id/retire` | 过时/被反驳 → retired |
| `DELETE /admin/memories/:id` | 彻底删除 |

开关：`MEMORY_ENABLED=false`（默认）。停用时走 NoopStore——端点照常返回空结果，客户端与面板无需感知开关。

## 4. 客户端接入（通道 B，推荐）

个人 agent 场景每个客户端的改造成本要趋近于零，因此 v1 只要求一个最薄 wrapper：

1. 启动时按 `git remote get-url origin` 推导 `scope_key`（如 `github:hwj123hwj/litellm-gateway`）；
2. 请求前 `GET /v1/memory?client=<名字>&project=<scope_key>`，把 `block` 拼进 system 末尾；
3. `POST /v1/memory` 上报候选记忆（可选）。

显式文件（各仓库 AGENTS.md）仍是第一优先记忆——本层只收它覆盖不了的跨客户端、跨仓库条目，两者不重复注入。

## 5. 路线图

- **P2 请求路径注入（通道 A 兜底）**：`MEMORY_INJECT=true` 时在 chat/messages/responses 处理器内，按请求头 `X-Memory-Client` / `X-Memory-Project` 注入短记忆块；与客户端自带记忆功能互斥检测，避免双份注入。
- **P3 自动提取管道**：后台任务消费 `conversation_archives`，用网关自有 Provider 调 LLM 抽取候选命题（带写入门槛：以后还用得到吗？与现有条目冲突吗？），以 `candidate` 落库走人工确认。归档是原料、候选是产物，仍然符合 R7 分工。
- **P4 平台事件锚点**：GitHub fine-grained PAT（只读）轮询个人仓库的 push/PR 事件（或 GitLab webhook，视仓库托管方）：MR 合入 → 时间窗口内同作用域候选记忆置信度上调并落 `git:<sha>` 锚点。公司仓库（git.seres.cn）作用域默认**排除提取**，避免工作上下文进入个人记忆库。
- **P5 遗忘机制**：基于 hit_count/last_hit_at 的降权与清理任务（如 90 天零命中 → 自动 retired）。

## 6. 明确不做

- 不做向量库检索起步：精确约定用精确查询；等自然语言知识规模上来再评估语义检索；
- 不做多租户：作用域键里没有租户维度，这是单用户基础设施；
- 不在网关做知识编译/清洗：那是 agent-lessons 的职责（R7 分工）。
