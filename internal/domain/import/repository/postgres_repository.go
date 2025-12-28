package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresImportRepository implements ImportRepository using PostgreSQL
type PostgresImportRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresImportRepository creates a new PostgreSQL-backed import repository
func NewPostgresImportRepository(pool *pgxpool.Pool) *PostgresImportRepository {
	return &PostgresImportRepository{pool: pool}
}

// GetMappingByFingerprint looks up a bank mapping by its fingerprint
// First checks user-specific mappings, then falls back to global templates
func (r *PostgresImportRepository) GetMappingByFingerprint(ctx context.Context, fingerprint string, userID *uuid.UUID) (*BankMapping, error) {
	query := `
		SELECT id, user_id, fingerprint, bank_name, delimiter, skip_lines, date_format,
		       date_col, desc_col, category_col, amount_col, debit_col, credit_col,
		       is_european_format, created_at, updated_at
		FROM bank_mappings
		WHERE fingerprint = $1 AND (user_id = $2 OR user_id IS NULL)
		ORDER BY user_id NULLS LAST
		LIMIT 1
	`

	var mapping BankMapping
	err := r.pool.QueryRow(ctx, query, fingerprint, userID).Scan(
		&mapping.ID, &mapping.UserID, &mapping.Fingerprint, &mapping.BankName,
		&mapping.Delimiter, &mapping.SkipLines, &mapping.DateFormat,
		&mapping.DateCol, &mapping.DescCol, &mapping.CategoryCol,
		&mapping.AmountCol, &mapping.DebitCol, &mapping.CreditCol,
		&mapping.IsEuropeanFormat, &mapping.CreatedAt, &mapping.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get mapping by fingerprint: %w", err)
	}

	return &mapping, nil
}

// CreateMapping inserts a new bank mapping
func (r *PostgresImportRepository) CreateMapping(ctx context.Context, mapping *BankMapping) error {
	if mapping.ID == uuid.Nil {
		mapping.ID = uuid.New()
	}

	query := `
		INSERT INTO bank_mappings (
			id, user_id, fingerprint, bank_name, delimiter, skip_lines, date_format,
			date_col, desc_col, category_col, amount_col, debit_col, credit_col,
			is_european_format
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`

	_, err := r.pool.Exec(ctx, query,
		mapping.ID, mapping.UserID, mapping.Fingerprint, mapping.BankName,
		mapping.Delimiter, mapping.SkipLines, mapping.DateFormat,
		mapping.DateCol, mapping.DescCol, mapping.CategoryCol,
		mapping.AmountCol, mapping.DebitCol, mapping.CreditCol,
		mapping.IsEuropeanFormat,
	)
	if err != nil {
		return fmt.Errorf("failed to create bank mapping: %w", err)
	}

	return nil
}

// UpdateMapping updates an existing bank mapping
func (r *PostgresImportRepository) UpdateMapping(ctx context.Context, mapping *BankMapping) error {
	query := `
		UPDATE bank_mappings SET
			bank_name = $2, delimiter = $3, skip_lines = $4, date_format = $5,
			date_col = $6, desc_col = $7, category_col = $8, amount_col = $9,
			debit_col = $10, credit_col = $11, is_european_format = $12,
			updated_at = NOW()
		WHERE id = $1
	`

	_, err := r.pool.Exec(ctx, query,
		mapping.ID, mapping.BankName, mapping.Delimiter, mapping.SkipLines, mapping.DateFormat,
		mapping.DateCol, mapping.DescCol, mapping.CategoryCol, mapping.AmountCol,
		mapping.DebitCol, mapping.CreditCol, mapping.IsEuropeanFormat,
	)
	if err != nil {
		return fmt.Errorf("failed to update bank mapping: %w", err)
	}

	return nil
}

// ListUserMappings returns all mappings for a user (including global templates)
func (r *PostgresImportRepository) ListUserMappings(ctx context.Context, userID uuid.UUID) ([]*BankMapping, error) {
	query := `
		SELECT id, user_id, fingerprint, bank_name, delimiter, skip_lines, date_format,
		       date_col, desc_col, category_col, amount_col, debit_col, credit_col,
		       is_european_format, created_at, updated_at
		FROM bank_mappings
		WHERE user_id = $1 OR user_id IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list user mappings: %w", err)
	}
	defer rows.Close()

	var mappings []*BankMapping
	for rows.Next() {
		var m BankMapping
		err := rows.Scan(
			&m.ID, &m.UserID, &m.Fingerprint, &m.BankName,
			&m.Delimiter, &m.SkipLines, &m.DateFormat,
			&m.DateCol, &m.DescCol, &m.CategoryCol,
			&m.AmountCol, &m.DebitCol, &m.CreditCol,
			&m.IsEuropeanFormat, &m.CreatedAt, &m.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan bank mapping: %w", err)
		}
		mappings = append(mappings, &m)
	}

	return mappings, nil
}

// CreateUserFile inserts a new user file record
func (r *PostgresImportRepository) CreateUserFile(ctx context.Context, file *UserFile) error {
	if file.ID == uuid.Nil {
		file.ID = uuid.New()
	}

	query := `
		INSERT INTO user_files (id, user_id, type, mime_type, file_name, size_bytes, checksum_sha256, storage_url)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := r.pool.Exec(ctx, query,
		file.ID, file.UserID, file.Type, file.MimeType, file.FileName,
		file.SizeBytes, file.ChecksumSHA256, file.StorageURL,
	)
	if err != nil {
		return fmt.Errorf("failed to create user file: %w", err)
	}

	return nil
}

// GetUserFileByID retrieves a user file by ID
func (r *PostgresImportRepository) GetUserFileByID(ctx context.Context, id uuid.UUID) (*UserFile, error) {
	query := `
		SELECT id, user_id, type, mime_type, file_name, size_bytes, checksum_sha256, storage_url, created_at
		FROM user_files WHERE id = $1
	`

	var file UserFile
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&file.ID, &file.UserID, &file.Type, &file.MimeType, &file.FileName,
		&file.SizeBytes, &file.ChecksumSHA256, &file.StorageURL, &file.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user file: %w", err)
	}

	return &file, nil
}

// CreateImportJob creates a new import job
func (r *PostgresImportRepository) CreateImportJob(ctx context.Context, job *ImportJob) error {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}

	query := `
		INSERT INTO import_jobs (id, user_id, file_id, kind, status, account_id, timezone, date_format, rows_total)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := r.pool.Exec(ctx, query,
		job.ID, job.UserID, job.FileID, job.Kind, job.Status,
		job.AccountID, job.Timezone, job.DateFormat, job.RowsTotal,
	)
	if err != nil {
		return fmt.Errorf("failed to create import job: %w", err)
	}

	return nil
}

// GetImportJobByID retrieves an import job by ID
func (r *PostgresImportRepository) GetImportJobByID(ctx context.Context, id uuid.UUID) (*ImportJob, error) {
	query := `
		SELECT id, user_id, file_id, kind, status, account_id, timezone, date_format,
		       error_message, rows_total, rows_imported, rows_failed,
		       requested_at, started_at, finished_at
		FROM import_jobs WHERE id = $1
	`

	var job ImportJob
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&job.ID, &job.UserID, &job.FileID, &job.Kind, &job.Status,
		&job.AccountID, &job.Timezone, &job.DateFormat,
		&job.ErrorMessage, &job.RowsTotal, &job.RowsImported, &job.RowsFailed,
		&job.RequestedAt, &job.StartedAt, &job.FinishedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get import job: %w", err)
	}

	return &job, nil
}

// UpdateImportJobProgress updates the row counts for an import job
func (r *PostgresImportRepository) UpdateImportJobProgress(ctx context.Context, id uuid.UUID, rowsImported, rowsFailed int) error {
	query := `UPDATE import_jobs SET rows_imported = $2, rows_failed = $3 WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id, rowsImported, rowsFailed)
	if err != nil {
		return fmt.Errorf("failed to update import job progress: %w", err)
	}
	return nil
}

// UpdateImportJobStatus updates the status of an import job
func (r *PostgresImportRepository) UpdateImportJobStatus(ctx context.Context, id uuid.UUID, status string, errorMessage *string) error {
	query := `UPDATE import_jobs SET status = $2, error_message = $3 WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id, status, errorMessage)
	if err != nil {
		return fmt.Errorf("failed to update import job status: %w", err)
	}
	return nil
}

// FinishImportJob marks an import job as complete
func (r *PostgresImportRepository) FinishImportJob(ctx context.Context, id uuid.UUID, status string, rowsImported, rowsFailed int, errorMessage *string) error {
	query := `
		UPDATE import_jobs SET
			status = $2, rows_imported = $3, rows_failed = $4,
			error_message = $5, finished_at = NOW(), rows_total = $3 + $4
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, id, status, rowsImported, rowsFailed, errorMessage)
	if err != nil {
		return fmt.Errorf("failed to finish import job: %w", err)
	}
	return nil
}

// BulkInsertTransactions inserts multiple transactions efficiently
func (r *PostgresImportRepository) BulkInsertTransactions(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID, txs []*ParsedTransaction) (int, error) {
	if len(txs) == 0 {
		return 0, nil
	}

	// Use COPY for bulk insert performance
	columns := []string{"id", "user_id", "account_id", "posted_at", "description", "original_description", "amount_minor", "currency_code", "source", "external_id"}

	copyCount, err := r.pool.CopyFrom(ctx,
		pgx.Identifier{"transactions"},
		columns,
		pgx.CopyFromSlice(len(txs), func(i int) ([]any, error) {
			tx := txs[i]
			// Generate external_id from hash of transaction data for deduplication
			externalID := generateExternalID(tx)
			return []any{
				uuid.New(),     // id
				userID,         // user_id
				accountID,      // account_id
				tx.Date,        // posted_at
				tx.Description, // description
				tx.Description, // original_description
				tx.AmountCents, // amount_minor
				"EUR",          // currency_code (default, could be configurable)
				"csv",          // source
				externalID,     // external_id
			}, nil
		}),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to bulk insert transactions: %w", err)
	}

	return int(copyCount), nil
}

// generateExternalID creates a unique identifier for deduplication
func generateExternalID(tx *ParsedTransaction) string {
	data := fmt.Sprintf("%s|%s|%d", tx.Date.Format(time.RFC3339), tx.Description, tx.AmountCents)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:16]) // First 16 bytes for reasonable length
}
