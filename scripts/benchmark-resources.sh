#!/bin/bash

echo "=== LiteLLM 网关 - 资源占用对比测试 ==="
echo ""
echo "本脚本会对比有/无 PostgreSQL 的内存占用"
echo ""

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

cleanup() {
    echo ""
    echo "清理测试容器..."
    docker stop litellm db 2>/dev/null || true
    docker rm litellm db 2>/dev/null || true
    docker network rm gateway 2>/dev/null || true
}

trap cleanup EXIT

# 创建网络
docker network create gateway 2>/dev/null || true

echo -e "${BLUE}=== 测试 1: 仅 LiteLLM（无数据库）===${NC}"
echo "启动 LiteLLM 容器..."

if [ ! -f ~/.litellm/.env ]; then
    echo "❌ 需要 ~/.litellm/.env 文件"
    exit 1
fi

docker run -d \
    --name litellm \
    --network gateway \
    -p 4000:4000 \
    -v ~/.litellm/config.yaml:/app/config.yaml \
    --env-file ~/.litellm/.env \
    ghcr.io/berriai/litellm:main-latest \
    --config /app/config.yaml \
    --port 4000 > /dev/null

echo "⏳ 等待 15 秒让容器稳定..."
sleep 15

echo -e "${GREEN}无数据库模式 - 容器资源占用:${NC}"
docker stats --no-stream litellm --format "table {{.Container}}\t{{.MemUsage}}\t{{.CPUPerc}}"

echo ""
echo "按 Enter 继续下一个测试..."
read

docker stop litellm
docker rm litellm

echo ""
echo -e "${BLUE}=== 测试 2: LiteLLM + PostgreSQL ===${NC}"
echo "启动 PostgreSQL 数据库..."

docker run -d \
    --name db \
    --network gateway \
    -e POSTGRES_USER=litellm \
    -e POSTGRES_PASSWORD=litellm123 \
    -e POSTGRES_DB=litellm \
    postgres:16-alpine > /dev/null

echo "启动 LiteLLM 容器（连接数据库）..."
sleep 3

docker run -d \
    --name litellm \
    --network gateway \
    -p 4000:4000 \
    -v ~/.litellm/config.yaml:/app/config.yaml \
    --env-file ~/.litellm/.env \
    -e DATABASE_URL=postgresql://litellm:litellm123@db:5432/litellm \
    ghcr.io/berriai/litellm:main-latest \
    --config /app/config.yaml \
    --port 4000 > /dev/null

echo "⏳ 等待 15 秒让容器稳定..."
sleep 15

echo -e "${GREEN}有数据库模式 - 容器资源占用:${NC}"
docker stats --no-stream db litellm --format "table {{.Container}}\t{{.MemUsage}}\t{{.CPUPerc}}"

echo ""
echo -e "${YELLOW}=== 资源占用对比总结 ===${NC}"
echo ""
echo "测试说明："
echo "  - MemUsage: 容器当前内存占用（不包括 swap）"
echo "  - CPUPerc: 当前 CPU 使用率"
echo ""
echo "💡 建议："
echo "  - 本地开发：使用无数据库版本（节省 ~200 MB 内存）"
echo "  - 生产环境：使用完整版本（需要数据库持久化）"
echo ""
echo "启动脚本："
echo "  • 无数据库：./scripts/start-local-no-db.sh"
echo "  • 完整版本：docker-compose up -d"
echo ""
