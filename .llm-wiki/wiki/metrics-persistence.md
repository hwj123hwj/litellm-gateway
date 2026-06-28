# Metrics Persistence

## 概述

指标持久化模块，使用 SQLite 存储请求日志和每日统计数据，确保重启后数据不丢失。

## 架构

```
请求 → Logging Middleware → Collector (内存)
                                ↓
                          SQLite Store (持久化)
```

## 数据库结构

### request_logs 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER | 自增主键 |
| timestamp | DATETIME | 请求时间 |
| method | TEXT | HTTP 方法 |
| path | TEXT | 请求路径 |
| model | TEXT | 模型名称 |
| provider | TEXT | 提供商 |
| status_code | INTEGER | HTTP 状态码 |
| latency_ms | REAL | 延迟（毫秒） |
| input_tokens | INTEGER | 输入 token 数 |
| output_tokens | INTEGER | 输出 token 数 |
| is_stream | BOOLEAN | 是否流式 |
| error | TEXT | 错误信息 |

### daily_stats 表

| 字段 | 类型 | 说明 |
|------|------|------|
| date | TEXT | 日期（YYYY-MM-DD） |
| model | TEXT | 模型名称 |
| provider | TEXT | 提供商 |
| requests | INTEGER | 请求数 |
| successes | INTEGER | 成功数 |
| errors | INTEGER | 失败数 |
| total_tokens | INTEGER | 总 token 数 |
| total_latency_ms | REAL | 总延迟 |

## 配置

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `PI_GO_DATA_DIR` | `./data` | 数据目录 |

SQLite 数据库文件：`{PI_GO_DATA_DIR}/metrics.db`

## 特性

- **WAL 模式**：启用 Write-Ahead Logging，提高并发写入性能
- **异步写入**：`SaveRecord` 在 goroutine 中执行，不阻塞主请求
- **自动清理**：`Cleanup(days)` 方法清理旧数据
- **优雅降级**：SQLite 初始化失败时回退到纯内存模式
