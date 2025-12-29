-- +goose Up
-- +goose StatementBegin

-- Add institution_name to track which bank/institution the import came from
ALTER TABLE import_jobs ADD COLUMN institution_name TEXT;

ALTER TABLE transactions ADD COLUMN institution_name TEXT;

-- Index for filtering by institution
CREATE INDEX idx_transactions_institution_name ON transactions (institution_name)
WHERE
    institution_name IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_transactions_institution_name;

ALTER TABLE transactions DROP COLUMN IF EXISTS institution_name;

ALTER TABLE import_jobs DROP COLUMN IF EXISTS institution_name;

-- +goose StatementEnd