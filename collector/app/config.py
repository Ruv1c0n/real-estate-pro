from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    # режим для защиты: true = читаем только локальный JSON-дамп
    mock_mode: bool = True
    mock_data_path: str = "/srv/data/listings.json"

    # живые источники, включаются когда mock_mode=false (через запятую)
    live_sources: str = "cian"
    # если живой источник заблокировал нас, подменяем его моком
    fallback_to_mock: bool = True

    core_api_url: str = "http://core-api:8000"
    collect_interval_sec: int = 300
    http_timeout_sec: float = 10.0
    request_delay_sec: float = 0.0  # пауза между POST, чтобы не заливать API

    # курсы к базовой валюте (USD). Для демо статичны; переопределяются через
    # переменную окружения FX_RATES='{"EUR": 1.1}'
    fx_rates: dict[str, float] = {
        "USD": 1.0,
        "EUR": 1.08,
        "BYN": 0.31,
        "RUB": 0.011,
        "KZT": 0.002,
        "PLN": 0.25,
        "GEL": 0.37,
        "TRY": 0.029,
        "AED": 0.272,
    }


settings = Settings()
