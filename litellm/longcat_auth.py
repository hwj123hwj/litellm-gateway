import os
import litellm
from litellm.integrations.custom_logger import CustomLogger
from litellm.proxy._types import UserAPIKeyAuth


LONGCAT_MODELS = {"longcat-sonnet", "longcat-opus"}


class LongCatAuthRewriter(CustomLogger):
    """Rewrite x-api-key to Authorization: Bearer for longcat provider.

    LiteLLM sends x-api-key for anthropic/ provider, but api.longcat.chat
    only accepts Authorization: Bearer header.
    """

    async def async_pre_call_hook(
        self,
        user_api_key_dict: UserAPIKeyAuth,
        cache,
        data: dict,
        call_type,
    ):
        model = data.get("model", "")
        if model not in LONGCAT_MODELS:
            return None

        api_key = os.environ.get("LONGCAT_API_KEY", "")
        if not api_key:
            return None

        extra_headers = data.get("extra_headers", {})
        extra_headers["Authorization"] = f"Bearer {api_key}"
        if "x-api-key" in extra_headers:
            del extra_headers["x-api-key"]
        data["extra_headers"] = extra_headers

        litellm.verbose_logger.debug(
            f"LongCatAuthRewriter: injected Authorization header for model={model}"
        )

        return data


longcat_auth_rewriter = LongCatAuthRewriter()
