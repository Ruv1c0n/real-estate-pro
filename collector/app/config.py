from typing import Literal

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Настройки Collector.

    Реквизиты источника и ключ администратора задаются через .env (НФТ-1).
    MOCK_MODE=true (по умолчанию) включает локальные данные; при
    MOCK_MODE=false нужны SOURCE_API_URL и SOURCE_API_TOKEN — если их нет,
    сервис сообщает об ошибке и переходит на mock (ФТ-1.2).
    Переменные окружения имеют приоритет над файлом .env.
    """

    app_name: str = "Real Estate Pro - Collector"
    app_version: str = "0.1.0"
    log_level: Literal["DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"] = "INFO"

    core_api_url: str = Field(
        default="http://127.0.0.1:8000", validation_alias="CORE_API_URL"
    )
    redis_url: str = Field(
        default="redis://localhost:6379/0", validation_alias="REDIS_URL"
    )
    # Пустой ключ открывает /collect/* — dev-режим, предупреждение в логе
    # при старте (то же соглашение, что у /debug/event в notifier).
    admin_key: str = Field(default="", validation_alias="ADMIN_KEY")

    mock_mode: bool = Field(default=True, validation_alias="MOCK_MODE")
    mock_publish_mode: Literal["batch", "stream"] = Field(
        default="batch", validation_alias="MOCK_PUBLISH_MODE"
    )
    mock_dump_path: str = Field(
        default="data/mock_dump.json", validation_alias="MOCK_DUMP_PATH"
    )
    mock_stream_min_seconds: int = Field(
        default=5, validation_alias="MOCK_STREAM_MIN_SECONDS"
    )
    mock_stream_max_seconds: int = Field(
        default=30, validation_alias="MOCK_STREAM_MAX_SECONDS"
    )

    # Реквизиты источника при MOCK_MODE=false (НФТ-1). Схема ответа,
    # совместимая с mock-avito (PR #62), описана в AvitoApiAdapter.
    source_api_url: str | None = Field(default=None, validation_alias="SOURCE_API_URL")
    source_api_token: str | None = Field(
        default=None, validation_alias="SOURCE_API_TOKEN"
    )
    source_api_timeout_seconds: float = Field(
        default=15.0, validation_alias="SOURCE_API_TIMEOUT_SECONDS"
    )

    # Период пакетного сбора (ФТ-1.4): 900 с = 15 минут по умолчанию.
    collect_interval_seconds: int = Field(
        default=900, validation_alias="COLLECT_INTERVAL_SECONDS"
    )
    core_api_timeout_seconds: float = Field(
        default=10.0, validation_alias="CORE_API_TIMEOUT_SECONDS"
    )
    # Повторы после начальной попытки (ФТ-1.6): до пяти.
    core_api_max_retries: int = Field(
        default=5, validation_alias="CORE_API_MAX_RETRIES"
    )

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
        hide_input_in_errors=True,
    )


settings = Settings()
