-- +goose Up
CREATE UNIQUE INDEX sessions_one_open_per_plate_key ON sessions (lot_id, translate(plate, 'ODQILT7BSZGAEVHN', '0001111852643UMM')) WHERE status = 'open';

-- +goose Down
DROP INDEX sessions_one_open_per_plate_key;
