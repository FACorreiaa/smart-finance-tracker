-- +goose Up
-- +goose StatementBegin

-- Bank mappings table to store learned CSV/TSV formats
-- Allows Echo to automatically recognize and process known bank formats
CREATE TABLE bank_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- NULL user_id = global template (pre-configured for major banks)
    -- Non-null = user-specific learned mapping
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,

-- Fingerprint = SHA256 hash of normalized header names
-- Used to quickly identify if we've seen this format before
fingerprint TEXT NOT NULL,

-- Human-readable bank name (e.g., "Caixa Geral de Depósitos")
bank_name VARCHAR(255),

-- Parsing configuration
delimiter CHAR(1)

NOT NULL DEFAULT ';', -- ';' (PT/EU), ',' (US), '\t' (TSV)
skip_lines INT NOT NULL DEFAULT 0, -- Lines of metadata before headers
date_format VARCHAR(32) NOT NULL DEFAULT 'DD-MM-YYYY', -- Date parsing pattern

-- Column indices (0-based)
date_col INT NOT NULL,
desc_col INT NOT NULL,
category_col INT, -- Optional: some banks include categories

-- Amount columns (mutually exclusive approaches)
-- Option 1: Single signed amount column
amount_col INT,
-- Option 2: Separate debit/credit columns (Portuguese banks like CGD)
debit_col INT,
credit_col INT,

-- Regional number format
-- TRUE = European: 1.234,56 (period thousands, comma decimal)
-- FALSE = American: 1,234.56 (comma thousands, period decimal)
is_european_format BOOLEAN NOT NULL DEFAULT TRUE,
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

-- Ensure fingerprint is unique per user (or globally if user_id is NULL)
CONSTRAINT bank_mappings_fingerprint_user_unique UNIQUE (fingerprint, user_id)
);

-- Index for fast fingerprint lookups
CREATE INDEX idx_bank_mappings_fingerprint ON bank_mappings (fingerprint);

CREATE INDEX idx_bank_mappings_user_id ON bank_mappings (user_id);

-- Trigger to update updated_at
CREATE TRIGGER trigger_set_bank_mappings_updated_at
BEFORE UPDATE ON bank_mappings
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Pre-populate with known Portuguese bank formats
-- Caixa Geral de Depósitos (CGD) - based on comprovativo.csv sample
INSERT INTO
    bank_mappings (
        user_id,
        fingerprint,
        bank_name,
        delimiter,
        skip_lines,
        date_format,
        date_col,
        desc_col,
        category_col,
        debit_col,
        credit_col,
        is_european_format
    )
VALUES (
        NULL, -- Global template
        'cgd_pt_standard_v1', -- Will be replaced with actual hash on first real import
        'Caixa Geral de Depósitos',
        ';',
        6, -- 6 lines of metadata before headers
        'DD-MM-YYYY',
        0, -- Data mov.
        2, -- Descrição
        7, -- Categoria (if present)
        3, -- Débito
        4, -- Crédito
        TRUE
    );

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trigger_set_bank_mappings_updated_at ON bank_mappings;

DROP TABLE IF EXISTS bank_mappings;
-- +goose StatementEnd