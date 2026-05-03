#!/bin/bash
set -e

DOMAIN=$1
EMAIL=$2

if [ -z "$DOMAIN" ] || [ -z "$EMAIL" ]; then
    echo "用法: ./scripts/init.sh <域名> <邮箱>"
    echo "示例: ./scripts/init.sh ai.yourdomain.com your@email.com"
    exit 1
fi

echo "=== 1. 创建目录结构 ==="
mkdir -p litellm nginx certbot/conf certbot/www

echo "=== 2. 初始化 .env ==="
if [ ! -f litellm/.env ]; then
    if [ -f litellm/.env.example ]; then
        cp litellm/.env.example litellm/.env
        echo "已从 .env.example 创建 .env，请编辑填入 API keys"
    else
        echo "错误: litellm/.env.example 不存在"
        exit 1
    fi
fi
chmod 600 litellm/.env

echo "=== 3. 创建 longcat_auth.py ==="
if [ ! -f litellm/longcat_auth.py ]; then
    cat > litellm/longcat_auth.py << 'PYEOF'
import os
import litellm
from litellm.integrations.custom_logger import CustomLogger
from litellm.proxy._types import UserAPIKeyAuth

LONGCAT_MODELS = {"longcat"}

class LongCatAuthRewriter(CustomLogger):
    """Rewrite x-api-key to Authorization: Bearer for longcat provider."""

    async def async_pre_call_hook(self, user_api_key_dict, cache, data, call_type):
        model = data.get("model", "")
        if model not in LONGCAT_MODELS:
            return None
        api_key = os.environ.get("LONGCAT_API_KEY", "")
        if not api_key:
            return None
        psh = data.get("provider_specific_header")
        if psh and isinstance(psh, dict):
            extra = dict(psh.get("extra_headers", {}))
            extra["authorization"] = f"Bearer {api_key}"
            extra["x-api-key"] = ""
            psh["extra_headers"] = extra
            data["provider_specific_header"] = psh
        else:
            data["provider_specific_header"] = {
                "extra_headers": {
                    "authorization": f"Bearer {api_key}",
                    "x-api-key": "",
                }
            }
        return data

longcat_auth_rewriter = LongCatAuthRewriter()
PYEOF
    echo "已创建 litellm/longcat_auth.py"
fi

echo "=== 4. 替换 nginx.conf 中的域名 ==="
if [ -f nginx/nginx.conf ]; then
    sed -i "s/your-domain.com/$DOMAIN/g" nginx/nginx.conf
    echo "已替换为: $DOMAIN"
else
    echo "错误: nginx/nginx.conf 不存在"
    exit 1
fi

echo "=== 5. 签发 SSL 证书 ==="
docker compose run --rm certbot certonly \
    --webroot \
    --webroot-path /var/www/certbot \
    --email "$EMAIL" \
    --agree-tos \
    --no-eff-email \
    -d "$DOMAIN"

echo "=== 6. 启动服务 ==="
docker compose up -d

echo "=== 7. 配置防火墙 ==="
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw --force enable

echo ""
echo "========================================="
echo "  部署完成！"
echo "========================================="
echo ""
echo "  网关地址: https://$DOMAIN"
echo ""
echo "  下一步:"
echo "  1. 编辑 litellm/.env 填入 API keys"
echo "  2. 运行 docker compose restart litellm"
echo "  3. 在 Claude Code 中配置:"
echo "     ANTHROPIC_BASE_URL=https://$DOMAIN/v1"
echo "     ANTHROPIC_AUTH_TOKEN=<你的 LITELLM_MASTER_KEY>"
echo ""
