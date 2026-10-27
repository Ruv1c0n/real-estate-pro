from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    app_name: str = "Real Estate Pro - Core API"
    app_version: str = "0.1.0"

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        env_prefix="CORE_API_",
        extra="ignore",
    )


settings = Settings()