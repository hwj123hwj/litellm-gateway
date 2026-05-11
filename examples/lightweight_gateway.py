#!/usr/bin/env python3
"""
轻量级 LLM 网关 - 替代 LiteLLM 的最小化实现
内存占用：50-80 MB（vs LiteLLM 的 200-300 MB）

这是一个演示项目，展示如何用 200 行代码替代 LiteLLM
"""

import os
import json
import asyncio
from typing import Dict, List, Optional
from fastapi import FastAPI, HTTPException, Header, Request
from fastapi.responses import StreamingResponse
import httpx
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# ============ 配置 ============

PROVIDERS = {
    # 智谱 BigModel（主力）
    "glm-haiku": {
        "url": "https://open.bigmodel.cn/api/anthropic/v1/messages",
        "api_key_env": "GLM_API_KEY"
    },
    "glm-sonnet": {
        "url": "https://open.bigmodel.cn/api/anthropic/v1/messages",
        "api_key_env": "GLM_API_KEY"
    },
    "glm-opus": {
        "url": "https://open.bigmodel.cn/api/anthropic/v1/messages",
        "api_key_env": "GLM_API_KEY"
    },

    # 小米 MiMo（备用1）
    "mimo-haiku": {
        "url": "https://token-plan-cn.xiaomimimo.com/anthropic/v1/messages",
        "api_key_env": "MIMO_API_KEY"
    },
    "mimo-sonnet": {
        "url": "https://token-plan-cn.xiaomimimo.com/anthropic/v1/messages",
        "api_key_env": "MIMO_API_KEY"
    },
    "mimo-opus": {
        "url": "https://token-plan-cn.xiaomimimo.com/anthropic/v1/messages",
        "api_key_env": "MIMO_API_KEY"
    },

    # 美团 LongCat（备用2）
    "longcat": {
        "url": "https://api.longcat.chat/anthropic/v1/messages",
        "api_key_env": "LONGCAT_API_KEY",
        "use_bearer": True  # 美团使用 Bearer token
    },
}

# Fallback 链
FALLBACK_CHAINS = {
    "coding": ["glm-sonnet", "mimo-sonnet", "longcat"],
    "glm-haiku": ["glm-haiku"],
    "glm-sonnet": ["glm-sonnet"],
    "glm-opus": ["glm-opus"],
    "mimo-haiku": ["mimo-haiku"],
    "mimo-sonnet": ["mimo-sonnet"],
    "mimo-opus": ["mimo-opus"],
    "longcat": ["longcat"],
}

MASTER_KEY = os.environ.get("LITELLM_MASTER_KEY", "sk-local-gateway-test")

app = FastAPI(title="Lightweight LLM Gateway")


# ============ 身份验证 ============

@app.middleware("http")
async def check_auth(request: Request, call_next):
    """检查每个请求的认证"""
    if request.url.path == "/health":
        return await call_next(request)

    auth_header = request.headers.get("authorization", "")
    if not auth_header.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Missing or invalid auth header")

    token = auth_header.split(" ")[1]
    if token != MASTER_KEY:
        raise HTTPException(status_code=401, detail="Invalid token")

    return await call_next(request)


# ============ 核心路由逻辑 ============

async def forward_to_provider(
    model: str,
    provider_url: str,
    api_key_env: str,
    request_data: dict,
    use_bearer: bool = False
) -> dict:
    """转发请求到上游提供商"""

    api_key = os.environ.get(api_key_env)
    if not api_key:
        raise HTTPException(status_code=500, detail=f"Missing API key: {api_key_env}")

    # 构建请求头
    headers = {
        "Content-Type": "application/json",
        "User-Agent": "LightweightGateway/1.0"
    }

    if use_bearer:
        headers["Authorization"] = f"Bearer {api_key}"
    else:
        headers["x-api-key"] = api_key

    # 替换模型名
    request_data["model"] = model

    try:
        async with httpx.AsyncClient(timeout=120) as client:
            response = await client.post(
                provider_url,
                json=request_data,
                headers=headers
            )

            if response.status_code != 200:
                logger.error(f"Provider error: {response.status_code} {response.text}")
                return None

            return response.json()

    except Exception as e:
        logger.error(f"Request failed: {e}")
        return None


# ============ API 端点 ============

@app.get("/health")
async def health():
    """健康检查"""
    return {"status": "ok"}


@app.get("/v1/models")
async def list_models():
    """列出所有可用模型"""
    return {
        "object": "list",
        "data": [
            {"id": model, "object": "model"}
            for model in PROVIDERS.keys()
        ]
    }


@app.post("/v1/messages")
async def messages(request: Request):
    """转发消息请求"""
    request_data = await request.json()

    model_or_group = request_data.get("model")
    if not model_or_group:
        raise HTTPException(status_code=400, detail="Model is required")

    # 获取 fallback 链
    fallback_chain = FALLBACK_CHAINS.get(model_or_group)
    if not fallback_chain:
        raise HTTPException(status_code=400, detail=f"Unknown model: {model_or_group}")

    # 尝试每个提供商
    last_error = None
    for model in fallback_chain:
        if model not in PROVIDERS:
            continue

        provider_config = PROVIDERS[model]
        logger.info(f"Trying model: {model}")

        result = await forward_to_provider(
            model=model,
            provider_url=provider_config["url"],
            api_key_env=provider_config["api_key_env"],
            request_data=request_data.copy(),
            use_bearer=provider_config.get("use_bearer", False)
        )

        if result:
            return result

        last_error = f"Model {model} failed"

    # 所有提供商都失败了
    raise HTTPException(
        status_code=503,
        detail=f"All providers failed. Last error: {last_error}"
    )


@app.post("/v1/chat/completions")
async def chat_completions(request: Request):
    """OpenAI 兼容的聊天端点（可选）"""
    # 如果需要支持 OpenAI 格式，可以在这里做转换
    request_data = await request.json()

    # 简单的格式转换（演示）
    anthropic_data = {
        "model": request_data.get("model", "coding"),
        "messages": request_data.get("messages"),
        "max_tokens": request_data.get("max_tokens", 1024),
        "temperature": request_data.get("temperature", 1.0),
    }

    # 转发给 /v1/messages
    return await messages(Request(
        {"type": "http", "method": "POST", "body": json.dumps(anthropic_data)}
    ))


# ============ 启动命令 ============

if __name__ == "__main__":
    import uvicorn

    port = int(os.environ.get("PORT", 4000))
    logger.info(f"Starting gateway on port {port}")
    logger.info(f"Master key: {MASTER_KEY[:20]}...")
    logger.info(f"Providers: {', '.join(PROVIDERS.keys())}")

    uvicorn.run(
        app,
        host="0.0.0.0",
        port=port,
        log_level="info"
    )
