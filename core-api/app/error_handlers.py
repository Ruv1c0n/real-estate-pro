import logging

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from starlette.exceptions import HTTPException

logger = logging.getLogger(__name__)

ERROR_CODES = {
    400: "bad_request",
    401: "unauthorized",
    403: "forbidden",
    404: "not_found",
    405: "method_not_allowed",
    409: "conflict",
    422: "validation_error",
    429: "too_many_requests",
    500: "internal_error",
    503: "dependency_unavailable",
}


async def http_error_handler(
    request: Request,
    exc: HTTPException,
) -> JSONResponse:
    message = (
        exc.detail
        if isinstance(exc.detail, str)
        else "Ошибка запроса"
    )

    if exc.status_code == 404 and message == "Not Found":
        message = "Ресурс не найден"
    elif exc.status_code >= 500:
        message = "Сервис временно недоступен"

    return JSONResponse(
        status_code=exc.status_code,
        content={
            "error": ERROR_CODES.get(exc.status_code, "http_error"),
            "message": message,
        },
        headers=exc.headers,
    )


async def validation_error_handler(
    request: Request,
    exc: RequestValidationError,
) -> JSONResponse:
    errors = exc.errors()
    message = "Некорректные входные данные"

    if errors:
        field = ".".join(str(part) for part in errors[0]["loc"])
        message = f"Некорректное значение поля: {field}"

    return JSONResponse(
        status_code=422,
        content={
            "error": "validation_error",
            "message": message,
        },
    )


async def internal_error_handler(
    request: Request,
    exc: Exception,
) -> JSONResponse:
    logger.error(
        "Unhandled error: %s %s",
        request.method,
        request.url.path,
        exc_info=(type(exc), exc, exc.__traceback__),
    )

    return JSONResponse(
        status_code=500,
        content={
            "error": "internal_error",
            "message": "Внутренняя ошибка сервера",
        },
    )


def register_error_handlers(app: FastAPI) -> None:
    app.add_exception_handler(HTTPException, http_error_handler)
    app.add_exception_handler(
        RequestValidationError,
        validation_error_handler,
    )
    app.add_exception_handler(Exception, internal_error_handler)