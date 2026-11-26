from starlette._utils import get_route_path
from starlette.exceptions import HTTPException
from starlette.types import ASGIApp, Message, Receive, Scope, Send

MAX_LISTING_BODY_BYTES = 128 * 1024


class ListingBodyLimit:
    def __init__(self, app: ASGIApp) -> None:
        self.app = app

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        if (
            scope["type"] != "http"
            or scope["method"] != "POST"
            or get_route_path(scope).rstrip("/") != "/api/v1/listings"
        ):
            await self.app(scope, receive, send)
            return

        size = 0

        async def limited_receive() -> Message:
            nonlocal size
            message = await receive()
            if message["type"] == "http.request":
                size += len(message.get("body", b""))
                if size > MAX_LISTING_BODY_BYTES:
                    raise HTTPException(
                        413, "Тело объявления превышает допустимый размер"
                    )
            return message

        await self.app(scope, limited_receive, send)
