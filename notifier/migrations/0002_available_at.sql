-- 0002_available_at.sql — backoff для отложенной очереди (notifications_log).
--
-- available_at — время, начиная с которого deferred-задание можно снова
-- забирать флешеру. Вставка (Postpone) ставит now() через DEFAULT; повтор
-- (Mark retry) сдвигает available_at вперёд, PopBatch выбирает только
-- задания с available_at <= now(). Это убирает «тугую петлю» повторов:
-- сейчас строка с retry возвращается в выборку немедленно, следующим
-- проходом флешера (каждые 30 с).
--
-- Использование в коде notifier (зона Даниила, этот файл ничего не ломает):
--   PopBatch: WHERE status = 'deferred' AND available_at <= now()
--             ORDER BY available_at
--   Mark retry: UPDATE ... SET available_at = now() + <backoff>
-- Текущий SQL кода совместим без правок: DEFAULT now() даёт немедленную
-- доступность новых заданий, старый индекс заменён эквивалентным.

BEGIN;

ALTER TABLE notifier.notifications_log
    ADD COLUMN IF NOT EXISTS available_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Индекс выборки переезжает с created_at на available_at: порядок «самые
-- старые доступные первыми» теперь по доступности, а не по времени создания.
DROP INDEX IF EXISTS notifier.notifications_log_deferred_idx;
CREATE INDEX IF NOT EXISTS notifications_log_available_idx
    ON notifier.notifications_log (available_at)
    WHERE status = 'deferred';

COMMIT;
