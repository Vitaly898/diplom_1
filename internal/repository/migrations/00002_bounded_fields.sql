-- +goose Up
-- Login: 255 characters; bcrypt hash: 60 characters; order identifiers: 64 characters.
-- Check lengths first: PostgreSQL explicit varchar casts can silently truncate.
-- +goose StatementBegin
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM users WHERE char_length(login) > 255 OR char_length(password_hash) > 60)
 OR EXISTS (SELECT 1 FROM orders WHERE char_length(number) > 64)
 OR EXISTS (SELECT 1 FROM withdrawals WHERE char_length(order_number) > 64) THEN
  RAISE EXCEPTION 'Existing data exceeds the new field length limits';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE users ALTER COLUMN login TYPE VARCHAR(255), ALTER COLUMN password_hash TYPE VARCHAR(60);
ALTER TABLE orders ALTER COLUMN number TYPE VARCHAR(64);
ALTER TABLE withdrawals ALTER COLUMN order_number TYPE VARCHAR(64);
CREATE TYPE order_status AS ENUM ('NEW', 'PROCESSING', 'INVALID', 'PROCESSED');
ALTER TABLE orders ALTER COLUMN status DROP DEFAULT;
ALTER TABLE orders ALTER COLUMN status TYPE order_status USING status::order_status;
ALTER TABLE orders ALTER COLUMN status SET DEFAULT 'NEW'::order_status;

-- +goose Down
ALTER TABLE orders ALTER COLUMN status DROP DEFAULT;
ALTER TABLE orders ALTER COLUMN status TYPE TEXT USING status::text;
ALTER TABLE orders ALTER COLUMN status SET DEFAULT 'NEW';
DROP TYPE order_status;
ALTER TABLE users ALTER COLUMN login TYPE TEXT, ALTER COLUMN password_hash TYPE TEXT;
ALTER TABLE orders ALTER COLUMN number TYPE TEXT;
ALTER TABLE withdrawals ALTER COLUMN order_number TYPE TEXT;
