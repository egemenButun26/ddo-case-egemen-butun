-- +goose Up
ALTER TABLE "ya-reservations" ADD COLUMN idempotency_key text;
ALTER TABLE "ya-reservations" ADD COLUMN request_hash bytea;

ALTER TABLE "ya-reservations"
  ADD CONSTRAINT reservations_idempotency_key_uniq UNIQUE (idempotency_key);

ALTER TABLE "ya-reservations"
  ADD CONSTRAINT reservations_idempotency_chk CHECK (
    (idempotency_key IS NULL AND request_hash IS NULL)
    OR (
      idempotency_key IS NOT NULL
      AND char_length(idempotency_key) BETWEEN 1 AND 255
      AND octet_length(request_hash) = 32
    )
  );

-- +goose Down
ALTER TABLE "ya-reservations" DROP CONSTRAINT reservations_idempotency_chk;
ALTER TABLE "ya-reservations" DROP CONSTRAINT reservations_idempotency_key_uniq;
ALTER TABLE "ya-reservations" DROP COLUMN request_hash;
ALTER TABLE "ya-reservations" DROP COLUMN idempotency_key;
