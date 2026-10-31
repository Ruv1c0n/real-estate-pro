# Collector + UI (Дима)

Проверка одной командой из корня репозитория:

    docker compose -f dev/docker-compose.dev.yml up --build

- UI: http://localhost:8080
- Заглушка Core API: http://localhost:8000/docs
- В логах collector: "цикл завершён: {'created': 520, ... 'invalid': 2 ...}"

`dev/` это временная заглушка. Когда Илья выложит настоящий core-api, UI и collector
подключаются к нему через переменную CORE_API_URL (см. docker-compose.fragment.yml).
Контракты с Ильёй и Даниилом: CONTRACT.md.
