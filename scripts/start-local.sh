#!/bin/bash
set -e

echo "=== Claude Code 多模型网关 - 本地启动脚本 ==="
echo ""

echo "=== 1. 检查 Docker 是否运行 ==="
if ! docker ps > /dev/null 2>&1; then
    echo "❌ Docker 未运行，正在启动 OrbStack..."
    open -a OrbStack
    sleep 5
    echo "✅ OrbStack 已启动"
fi
echo "✅ Docker 运行正常"

echo ""
echo "=== 2. 检查配置文件 ==="
if [ ! -f ~/.litellm/config.yaml ]; then
    echo "❌ ~/.litellm/config.yaml 不存在"
    echo "请先复制配置文件："
    echo "  cp litellm/config.yaml ~/.litellm/"
    exit 1
fi
echo "✅ 配置文件存在"

if [ ! -f ~/.litellm/.env ]; then
    echo "❌ ~/.litellm/.env 不存在"
    echo "请先创建 .env 文件："
    echo "  cp litellm/.env.example ~/.litellm/.env"
    exit 1
fi
echo "✅ 环境变量文件存在"

echo ""
echo "=== 3. 数据库配置 ==="
echo "⚠️  本地开发模式：无需 PostgreSQL 数据库"
echo "    LiteLLM 将使用内存存储，适合开发测试"
echo "    （如需持久化日志和统计，请使用 docker-compose）"

echo ""
echo "=== 4. 检查 LiteLLM 容器状态 ==="
if docker ps -a --format '{{.Names}}' | grep -q "^litellm$"; then
    if docker ps --format '{{.Names}}' | grep -q "^litellm$"; then
        echo "✅ LiteLLM 容器已在运行"
        echo ""
        echo "访问地址: http://localhost:4000"
        echo "认证 Token: $(grep LITELLM_MASTER_KEY ~/.litellm/.env | cut -d= -f2)"
        exit 0
    else
        echo "📦 存在已停止的 litellm 容器，正在删除..."
        docker rm litellm
    fi
fi

echo ""
echo "=== 5. 启动 LiteLLM 容器 ==="
docker run -d \
    --name litellm \
    --network gateway \
    -p 4000:4000 \
    -v ~/.litellm/config.yaml:/app/config.yaml \
    -v ~/.litellm/longcat_auth.py:/app/longcat_auth/longcat_auth.py \
    --env-file ~/.litellm/.env \
    ghcr.io/berriai/litellm:main-latest \
    --config /app/config.yaml

echo ""
echo "=== 6. 等待服务启动 ==="
sleep 5

echo ""
echo "=== 7. 检查容器状态 ==="
if docker ps | grep -q litellm; then
    echo "✅ LiteLLM 容器启动成功！"
    echo ""
    echo "========================================="
    echo "  网关已启动"
    echo "========================================="
    echo ""
    echo "  访问地址: http://localhost:4000"
    echo "  认证 Token: $(grep LITELLM_MASTER_KEY ~/.litellm/.env | cut -d= -f2)"
    echo ""
    echo "  测试命令:"
    echo "  curl -X POST http://localhost:4000/v1/messages \\"
    echo "    -H \"Authorization: Bearer $(grep LITELLM_MASTER_KEY ~/.litellm/.env | cut -d= -f2)\" \\"
    echo "    -H \"Content-Type: application/json\" \\"
    echo "    -d '{\"model\":\"glm-sonnet\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],\"max_tokens\":10}'"
    echo ""
else
    echo "❌ 容器启动失败，查看日志："
    docker logs litellm --tail 20
    exit 1
fi