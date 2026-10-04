-- +goose Up
-- Миграция 00001: начальная схема накопительной системы лояльности.
-- Сервисные директивы миграций ниже — это специальные комментарии,
-- которые распознаёт goose.

-- Пользователи системы.
CREATE TABLE users (
    -- BIGINT GENERATED ALWAYS AS IDENTITY — автоинкрементный BIGINT,
    -- суррогатный первичный ключ.
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Логин уникален по требованиям задания. UNIQUE создаёт и индекс —
    -- поиск по логину при аутентификации будет быстрым. Гарантию
    -- уникальности даёт БД атомарно, а не наш код (защита от гонок).
    login TEXT NOT NULL UNIQUE,
    -- Храним только bcrypt-хеш пароля, никогда сам пароль.
    password_hash TEXT NOT NULL,
    -- TIMESTAMPTZ хранит момент времени с учётом таймзоны.
    -- DEFAULT now() — БД сама проставит время вставки.
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Заказы, загруженные пользователями для расчёта начислений.
CREATE TABLE orders (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Внешний ключ на владельца заказа. REFERENCES не даёт создать
    -- заказ для несуществующего пользователя (целостность данных).
    user_id BIGINT NOT NULL REFERENCES users (id),
    -- Номер заказа — СТРОКА: это идентификатор, а не число
    -- (не считаем арифметику; могут быть ведущие нули; длина произвольная).
    -- UNIQUE по всей таблице: номер может быть загружен только один раз —
    -- по этой ошибке поймаем "загружен другим пользователем" (409).
    number TEXT NOT NULL UNIQUE,
    -- Статус обработки: NEW / PROCESSING / INVALID / PROCESSED.
    -- Новый заказ всегда стартует с NEW.
    status TEXT NOT NULL DEFAULT 'NEW',
    -- Начисленные баллы. NULL, а не 0: NULL = "начисления ещё нет",
    -- 0 = "начисление рассчитано и равно нулю". Это разные состояния,
    -- и в JSON-ответе поле accrual должно ОТСУТСТВОВАТЬ, пока его нет.
    -- NUMERIC(12,2) — точное десятичное число: 12 цифр всего, 2 после точки.
    -- Деньги/баллы никогда не храним во FLOAT (0.1+0.2 != 0.3 во float!).
    accrual NUMERIC(12, 2),
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Индекс для выдачи заказов пользователя "от новых к старым".
-- Без него GET /api/user/orders делал бы полный обход таблицы.
CREATE INDEX idx_orders_user_uploaded ON orders (user_id, uploaded_at DESC);

-- Списания баллов в счёт оплаты заказов.
CREATE TABLE withdrawals (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users (id),
    -- Номер "гипотетического заказа", в счёт которого списали баллы.
    -- FK на orders НЕ делаем: этого заказа может не существовать в orders.
    order_number TEXT NOT NULL,
    -- Сумма списания. CHECK — ограничение уровня БД:
    -- отрицательное списание физически невозможно записать.
    sum NUMERIC(12, 2) NOT NULL CHECK (sum > 0),
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_withdrawals_user_processed ON withdrawals (user_id, processed_at DESC);

-- +goose Down
-- Откат: удаляем таблицы в обратном порядке (сначала зависимые).
DROP TABLE withdrawals;
DROP TABLE orders;
DROP TABLE users;
