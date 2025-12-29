// Package repository provides data access for import-related entities.
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// BankMapping represents a learned CSV/TSV format configuration
type BankMapping struct {
	ID               uuid.UUID  `db:"id"`
	UserID           *uuid.UUID `db:"user_id"` // NULL = global template
	Fingerprint      string     `db:"fingerprint"`
	BankName         *string    `db:"bank_name"`
	Delimiter        string     `db:"delimiter"`
	SkipLines        int        `db:"skip_lines"`
	DateFormat       string     `db:"date_format"`
	DateCol          int        `db:"date_col"`
	DescCol          int        `db:"desc_col"`
	CategoryCol      *int       `db:"category_col"`
	AmountCol        *int       `db:"amount_col"`
	DebitCol         *int       `db:"debit_col"`
	CreditCol        *int       `db:"credit_col"`
	IsEuropeanFormat bool       `db:"is_european_format"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`
}

// ImportJob tracks the status of a file import
type ImportJob struct {
	ID           uuid.UUID  `db:"id"`
	UserID       uuid.UUID  `db:"user_id"`
	FileID       uuid.UUID  `db:"file_id"`
	Kind         string     `db:"kind"`   // "transactions", "invoice"
	Status       string     `db:"status"` // "pending", "running", "succeeded", "failed"
	AccountID    *uuid.UUID `db:"account_id"`
	Timezone     *string    `db:"timezone"`
	DateFormat   *string    `db:"date_format"`
	ErrorMessage *string    `db:"error_message"`
	RowsTotal    int        `db:"rows_total"`
	RowsImported int        `db:"rows_imported"`
	RowsFailed   int        `db:"rows_failed"`
	RequestedAt  time.Time  `db:"requested_at"`
	StartedAt    *time.Time `db:"started_at"`
	FinishedAt   *time.Time `db:"finished_at"`
}

// UserFile represents an uploaded file
type UserFile struct {
	ID             uuid.UUID `db:"id"`
	UserID         uuid.UUID `db:"user_id"`
	Type           string    `db:"type"` // "csv", "xlsx", "pdf", "image"
	MimeType       string    `db:"mime_type"`
	FileName       string    `db:"file_name"`
	SizeBytes      int64     `db:"size_bytes"`
	ChecksumSHA256 *string   `db:"checksum_sha256"`
	StorageURL     *string   `db:"storage_url"`
	CreatedAt      time.Time `db:"created_at"`
}

// ParsedTransaction represents a transaction extracted from a file
type ParsedTransaction struct {
	Date        time.Time
	Description string
	AmountCents int64 // Signed: negative for expenses, positive for income
	Category    string
	ExternalID  string // For deduplication (e.g., row hash)
}

// ImportRepository defines data access operations for imports
type ImportRepository interface {
	// Bank Mappings
	GetMappingByFingerprint(ctx context.Context, fingerprint string, userID *uuid.UUID) (*BankMapping, error)
	CreateMapping(ctx context.Context, mapping *BankMapping) error
	UpdateMapping(ctx context.Context, mapping *BankMapping) error
	ListUserMappings(ctx context.Context, userID uuid.UUID) ([]*BankMapping, error)

	// Accounts
	GetAccountCurrency(ctx context.Context, userID uuid.UUID, accountID uuid.UUID) (string, error)

	// User Files
	CreateUserFile(ctx context.Context, file *UserFile) error
	GetUserFileByID(ctx context.Context, id uuid.UUID) (*UserFile, error)

	// Import Jobs
	CreateImportJob(ctx context.Context, job *ImportJob) error
	GetImportJobByID(ctx context.Context, id uuid.UUID) (*ImportJob, error)
	UpdateImportJobProgress(ctx context.Context, id uuid.UUID, rowsImported, rowsFailed int) error
	UpdateImportJobStatus(ctx context.Context, id uuid.UUID, status string, errorMessage *string) error
	FinishImportJob(ctx context.Context, id uuid.UUID, status string, rowsImported, rowsFailed int, errorMessage *string) error

	// Transactions (bulk insert for imported data)
	BulkInsertTransactions(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID, currencyCode string, importJobID uuid.UUID, institutionName string, txs []*ParsedTransaction) (int, error)

	// Transactions (list/query)
	ListTransactions(ctx context.Context, userID uuid.UUID, filter ListTransactionsFilter) ([]*Transaction, int64, error)

	// Transactions (delete by import job)
	DeleteByImportJobID(ctx context.Context, userID uuid.UUID, importJobID uuid.UUID) (int, error)
}

// Transaction represents a stored transaction with full metadata
type Transaction struct {
	ID                  uuid.UUID  `db:"id"`
	UserID              uuid.UUID  `db:"user_id"`
	AccountID           *uuid.UUID `db:"account_id"`
	CategoryID          *uuid.UUID `db:"category_id"`
	CategoryName        *string    `db:"category_name"` // Joined from categories table
	Date                time.Time  `db:"date"`
	Description         string     `db:"description"`
	MerchantName        *string    `db:"merchant_name"`
	OriginalDescription *string    `db:"original_description"`
	AmountCents         int64      `db:"amount_cents"`
	CurrencyCode        string     `db:"currency_code"`
	Source              string     `db:"source"`
	ExternalID          *string    `db:"external_id"`
	Notes               *string    `db:"notes"`
	InstitutionName     *string    `db:"institution_name"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
}

// ListTransactionsFilter specifies filter/pagination options for listing transactions
type ListTransactionsFilter struct {
	AccountID   *uuid.UUID
	CategoryID  *uuid.UUID
	ImportJobID *uuid.UUID // Filter by import batch (for staging view)
	StartDate   *time.Time
	EndDate     *time.Time
	Search      string // Search in description
	Limit       int
	Offset      int
}
