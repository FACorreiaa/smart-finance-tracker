-- +goose Up
-- +goose StatementBegin

-- Add import_job_id to track which import created each transaction
ALTER TABLE transactions
ADD COLUMN import_job_id UUID REFERENCES import_jobs (id) ON DELETE SET NULL;

-- Index for filtering transactions by import job (staging view)
CREATE INDEX idx_transactions_import_job_id ON transactions (import_job_id)
WHERE
    import_job_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_transactions_import_job_id;

ALTER TABLE transactions DROP COLUMN IF EXISTS import_job_id;

-- +goose StatementEnd