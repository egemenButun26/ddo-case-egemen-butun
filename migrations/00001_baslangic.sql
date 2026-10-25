-- +goose Up
CREATE TABLE app_meta (
    key text PRIMARY KEY,
    value text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO app_meta (key, value) VALUES ('schema_baslangic', '1');

-- +goose Down
DROP TABLE app_meta;
