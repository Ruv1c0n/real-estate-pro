from typing import Literal

from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict
from sqlalchemy.engine import make_url
from sqlalchemy.exc import ArgumentError


class Settings(BaseSettings):
    """Настройки Core API.

    DATABASE_URL и REDIS_URL читаются без префикса CORE_API_.
    Остальные настройки используют префикс CORE_API_.
    Переменные окружения имеют приоритет над файлом .env.
    """

    app_name: str = "Real Estate Pro - Core API"
    app_version: str = "0.1.0"
    log_level: Literal["DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"] = "INFO"

    database_url: str = Field(
        default="postgresql+asyncpg://app:app@127.0.0.1:5432/realestate",
        validation_alias="DATABASE_URL",
    )
    redis_url: str = Field(
        default="redis://localhost:6379/0",
        validation_alias="REDIS_URL",
    )

    @field_validator("database_url")
    @classmethod
    def validate_database_url(cls, value: str) -> str:
        message = (
            "DATABASE_URL должен быть корректным URL с драйвером postgresql+asyncpg"
        )

        try:
            url = make_url(value)
        except (ArgumentError, ValueError):
            raise ValueError(message) from None

        if url.drivername != "postgresql+asyncpg":
            raise ValueError(message)

        return value

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        env_prefix="CORE_API_",
        extra="ignore",
        hide_input_in_errors=True,
    )


settings = Settings()
