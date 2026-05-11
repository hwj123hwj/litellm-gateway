#!/bin/bash
set -e

echo "=== LiteLLM 网关 - 无数据库启动测试 ==="
echo ""

# 检查必要的环境变量
if [ ! -f ~/.litellm/.env ]; then
    echo "❌ ~/.litellm/.env 不存在"
    echo ""
    echo "请先运行以下命令准备配置："
    echo "  mkdir -p ~/.litellm"
    echo "  cp litellm/config.yaml ~/.litellm/"
    echo "  cp litellm/.env.example ~/.litellm/.env"
    echo ""
    echo "然后编辑 ~/.litellm/.env，填入你的实际 API 密钥"
    exit 1
fi

echo "✅ 环境变量文件已找到"
echo ""

# 检查是否有旧的 litellm 容器在运行
echo "检查现有容器..."
if docker ps --format '{{.Names}}' | grep -q "^litellm$"; then
    echo "发现已运行的 litellm 容器，正在停止..."
    docker stop litellm
    docker rm litellm
    sleep 2
fi

# 检查 litellm-db 是否还在运行（不需要它）
if docker ps --format '{{.Names}}' | grep -q "^litellm-db$"; then
    echo "⚠️  检测到 litellm-db 数据库容器仍在运行（轻量级模式不需要它）"
    echo "    正在停止 litellm-db..."
    docker stop litellm-db
    echo "✅ litellm-db 已停止（容器保留，如需恢复运行 docker start litellm-db）"
fi

echo "✅ 清理完成"
echo ""

# 创建网络（如果不存在）
if ! docker network ls | grep -q gateway; then
    echo "创建 Docker 网络..."
    docker network create gateway
    echo "✅ 网络已创建"
else
    echo "✅ Docker 网络已存在"
fi
echo ""

# 启动 LiteLLM 容器（无数据库依赖）
echo "=== 启动 LiteLLM 网关（无 PostgreSQL） ==="
docker run -d \
    --name litellm \
    --network gateway \
    -p 4000:4000 \
    -v ~/.litellm/config.yaml:/app/config.yaml \
    -v ~/.litellm/longcat_auth.py:/app/longcat_auth/longcat_auth.py \
    --env-file ~/.litellm/.env \
    ghcr.io/berriai/litellm:main-latest \
    --config /app/config.yaml \
    --port 4000

echo ""
echo "⏳ 等待服务启动（5秒）..."
sleep 5

# 检查容器是否正常运行
if docker ps | grep -q litellm; then
    echo "✅ LiteLLM 容器启动成功！"
    echo ""

    # 提取 token
    MASTER_KEY=$(grep LITELLM_MASTER_KEY ~/.litellm/.env | cut -d= -f2 || echo "UNKNOWN")

    echo "========================================="
    echo "  🚀 网关已启动（轻量级模式）"
    echo "========================================="
    echo ""
    echo "📍 访问地址: http://localhost:4000"
    echo "🔑 认证 Token: $MASTER_KEY"
    echo ""
    echo "💾 内存占用: ~200-300 MB（无数据库）"
    echo ""

    echo "📝 配置 Claude Code (~/.claude/settings.json):"
    echo "  {\"env\": {\"ANTHROPIC_BASE_URL\": \"http://localhost:4000/v1\", \"ANTHROPIC_AUTH_TOKEN\": \"$MASTER_KEY\"}}"
    echo ""

    echo "🧪 快速测试："
    echo "  curl -X POST http://localhost:4000/v1/messages \\"
    echo "    -H \"Authorization: Bearer $MASTER_KEY\" \\"
    echo "    -H \"Content-Type: application/json\" \\"
    echo "    -d '{\"model\":\"glm-sonnet\",\"max_tokens\":10,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'"
    echo ""

    echo "📊 查看实时日志："
    echo "  docker logs -f litellm"
    echo ""
    echo "🛑 停止网关："
    echo "  docker stop litellm"
    echo ""

    # 可选：自动测试一个模型
    echo "⏳ 检查网关 API 是否响应..."
    sleep 2

    RESPONSE=$(curl -s -X GET http://localhost:4000/v1/models \
        -H "Authorization: Bearer $MASTER_KEY" || echo "failed")

    if echo "$RESPONSE" | grep -q "data"; then
        echo "✅ API 响应正常"
        echo ""
        echo "🎉 所有检查通过！开始使用网关吧！"
    else
        echo "⚠️  API 暂未响应，查看日志："
        echo "  docker logs litellm"
    fi

else
    echo "❌ 容器启动失败！"
    echo ""
    echo "查看错误日志："
    docker logs litellm --tail 30
    echo ""
    exit 1
fi
