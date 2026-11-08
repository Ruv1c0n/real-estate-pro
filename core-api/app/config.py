from typing import Literal
from urllib.parse import quote

from pydantic import Field, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict
from sqlalchemy.engine import make_url
from sqlalchemy.exc import ArgumentError


class Settings(BaseSettings):
    """Настройки Core API.

    Подключение к PostgreSQL задаётся одним из двух способов:
    - DATABASE_URL — адрес целиком (удобно для локального запуска);
    - переменные POSTGRES_* — адрес собирается из частей, при этом
      пользователь, пароль и имя базы экранируются, поэтому спецсимволы
      (@, :, /) в пароле не ломают подключение. Так их передаёт compose.

    Если заданы оба, DATABASE_URL имеет приоритет.
    REDIS_URL читается без префикса CORE_API_.
    Остальные настройки используют префикс CORE_API_.
    Переменные окружения имеют приоритет над файлом .env.
    """

    app_name: str = "Real Estate Pro - Core API"
    app_version: str = "0.1.0"
    log_level: Literal["DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"] = "INFO"

    database_url: str | None = Field(
        default=None,
        validation_alias="DATABASE_URL",
    )
    postgres_user: str = Field(default="app", validation_alias="POSTGRES_USER")
    postgres_password: str = Field(default="app", validation_alias="POSTGRES_PASSWORD")
    postgres_host: str = Field(default="127.0.0.1", validation_alias="POSTGRES_HOST")
    postgres_port: int = Field(default=5432, validation_alias="POSTGRES_PORT")
    postgres_db: str = Field(default="realestate", validation_alias="POSTGRES_DB")
    redis_url: str = Field(
        default="redis://localhost:6379/0",
        validation_alias="REDIS_URL",
    )

    @model_validator(mode="after")
    def assemble_and_validate_database_url(self) -> "Settings":
        message = (
            "DATABASE_URL должен быть корректным URL с драйвером postgresql+asyncpg"
        )

        if self.database_url is None:
            self.database_url = (
                f"postgresql+asyncpg://"
                f"{quote(self.postgres_user, safe='')}:{quote(self.postgres_password, safe='')}"
                f"@{self.postgres_host}:{self.postgres_port}"
                f"/{quote(self.postgres_db, safe='')}"
            )

        try:
            url = make_url(self.database_url)
        except (ArgumentError, ValueError):
            raise ValueError(message) from None

        if url.drivername != "postgresql+asyncpg":
            raise ValueError(message)

        return self

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        env_prefix="CORE_API_",
        extra="ignore",
        hide_input_in_errors=True,
    )


settings = Settings()
