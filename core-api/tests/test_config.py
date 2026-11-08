import pytest
from app.config import Settings
from pydantic import ValidationError


@pytest.fixture(autouse=True)
def clean_settings_env(monkeypatch):
    names = [
        "DATABASE_URL",
        "REDIS_URL",
        "CORE_API_DATABASE_URL",
        "CORE_API_REDIS_URL",
        "CORE_API_APP_NAME",
        "CORE_API_APP_VERSION",
        "CORE_API_LOG_LEVEL",
    ]
    for name in names:
        monkeypatch.delenv(name, raising=False)


def test_database_url_accepts_asyncpg(monkeypatch):
    url = "postgresql+asyncpg://app:app@localhost:5432/realestate"
    monkeypatch.setenv("DATABASE_URL", url)

    settings = Settings(_env_file=None)

    assert settings.database_url == url


@pytest.mark.parametrize(
    "url",
    [
        "postgresql://app:app@localhost/db",
        "postgresql+psycopg://app:app@localhost/db",
        "postgresql+asyncpg_extra://app:app@localhost/db",
        "sqlite:///db",
        "not-a-url",
        "",
    ],
    ids=[
        "driver_not_specified",
        "different_driver",
        "similar_driver_name",
        "different_database",
        "malformed_url",
        "empty_url",
    ],
)
def test_database_url_rejects_wrong_driver(monkeypatch, url):
    monkeypatch.setenv("DATABASE_URL", url)

    with pytest.raises(ValidationError, match=r"postgresql\+asyncpg"):
        Settings(_env_file=None)


def test_database_url_validation_error_hides_password(monkeypatch):
    password = "s3cr3t"
    monkeypatch.setenv(
        "DATABASE_URL",
        f"postgresql://u:{password}@h/d",
    )

    with pytest.raises(ValidationError) as exc_info:
        Settings(_env_file=None)

    message = str(exc_info.value)
    assert "postgresql+asyncpg" in message
    assert password not in message
    assert "input_value=" not in message


def test_settings_defaults():
    settings = Settings(_env_file=None)

    assert settings.app_name == "Real Estate Pro - Core API"
    assert settings.app_version == "0.1.0"
    assert settings.log_level == "INFO"
    assert settings.database_url == (
        "postgresql+asyncpg://app:app@127.0.0.1:5432/realestate"
    )
    assert settings.redis_url == "redis://localhost:6379/0"


def test_settings_env_names(monkeypatch):
    monkeypatch.setenv(
        "CORE_API_DATABASE_URL",
        "postgresql+asyncpg://ignored:ignored@localhost/ignored",
    )
    monkeypatch.setenv("CORE_API_REDIS_URL", "redis://localhost:6399/9")

    settings = Settings(_env_file=None)

    assert settings.database_url == (
        "postgresql+asyncpg://app:app@127.0.0.1:5432/realestate"
    )
    assert settings.redis_url == "redis://localhost:6379/0"

    database_url = "postgresql+asyncpg://app:app@localhost:5433/test_db"
    redis_url = "redis://localhost:6380/1"
    monkeypatch.setenv("DATABASE_URL", database_url)
    monkeypatch.setenv("REDIS_URL", redis_url)
    monkeypatch.setenv("CORE_API_APP_NAME", "Test Core API")
    monkeypatch.setenv("CORE_API_APP_VERSION", "9.8.7")
    monkeypatch.setenv("CORE_API_LOG_LEVEL", "DEBUG")

    settings = Settings(_env_file=None)

    assert settings.database_url == database_url
    assert settings.redis_url == redis_url
    assert settings.app_name == "Test Core API"
    assert settings.app_version == "9.8.7"
    assert settings.log_level == "DEBUG"


def test_environment_overrides_dotenv(monkeypatch, tmp_path):
    env_file = tmp_path / ".env"
    env_file.write_text(
        "DATABASE_URL=postgresql+asyncpg://app:app@localhost/from_file\n"
        "REDIS_URL=redis://localhost:6380/2\n"
        "CORE_API_LOG_LEVEL=WARNING\n",
        encoding="utf-8",
    )

    database_url = "postgresql+asyncpg://app:app@localhost/from_environment"
    monkeypatch.setenv("DATABASE_URL", database_url)
    monkeypatch.setenv("CORE_API_LOG_LEVEL", "DEBUG")

    settings = Settings(_env_file=env_file)

    assert settings.database_url == database_url
    assert settings.log_level == "DEBUG"
    assert settings.redis_url == "redis://localhost:6380/2"
