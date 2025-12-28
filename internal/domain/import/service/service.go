// Package service provides the import orchestration logic.
package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/import/normalizer"
	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/import/repository"
	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/import/sniffer"
)

// ColumnMapping defines how to map CSV columns to transaction fields
type ColumnMapping struct {
	DateCol          int
	DescCol          int
	CategoryCol      int  // -1 if not available
	AmountCol        int  // For single amount column
	DebitCol         int  // For separate debit/credit
	CreditCol        int  // For separate debit/credit
	IsDoubleEntry    bool // True if using separate debit/credit columns
	IsEuropeanFormat bool // True for European number format (1.234,56)
	DateFormat       string
}

// AnalyzeResult contains the result of analyzing an uploaded file
type AnalyzeResult struct {
	// File analysis
	FileConfig        *sniffer.FileConfig
	ColumnSuggestions *sniffer.ColumnSuggestions

	// Existing mapping found
	MappingFound bool
	Mapping      *repository.BankMapping

	// If mapping found, these are set
	CanAutoImport bool
}

// ImportResult contains the result of an import operation
type ImportResult struct {
	JobID        uuid.UUID
	RowsTotal    int
	RowsImported int
	RowsFailed   int
	Errors       []string
}

// ImportService orchestrates file analysis and import operations
type ImportService struct {
	repo   repository.ImportRepository
	logger *slog.Logger
}

const (
	importBatchSize           = 500
	importProgressUpdateEvery = 500
)

type parseJob struct {
	lineNum int
	record  []string
}

type parseResult struct {
	lineNum int
	tx      *repository.ParsedTransaction
	err     error
}

// NewImportService creates a new import service
func NewImportService(repo repository.ImportRepository, logger *slog.Logger) *ImportService {
	return &ImportService{
		repo:   repo,
		logger: logger,
	}
}

// AnalyzeFile analyzes an uploaded CSV/TSV file and determines if it can be auto-imported
func (s *ImportService) AnalyzeFile(ctx context.Context, userID uuid.UUID, fileData []byte) (*AnalyzeResult, error) {
	// Step 1: Detect file configuration
	config, err := sniffer.DetectConfig(fileData)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze file: %w", err)
	}

	// Step 2: Get column suggestions
	suggestions := sniffer.SuggestColumns(config.Headers)

	// Step 3: Check for existing mapping
	mapping, err := s.repo.GetMappingByFingerprint(ctx, config.Fingerprint, &userID)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup mapping: %w", err)
	}

	result := &AnalyzeResult{
		FileConfig:        config,
		ColumnSuggestions: suggestions,
		MappingFound:      mapping != nil,
		Mapping:           mapping,
		CanAutoImport:     mapping != nil,
	}

	return result, nil
}

// SaveMapping saves a user's column mapping for future use
func (s *ImportService) SaveMapping(ctx context.Context, userID uuid.UUID, fingerprint string, bankName string, mapping ColumnMapping) error {
	bankNamePtr := &bankName
	if bankName == "" {
		bankNamePtr = nil
	}

	var categoryCol, amountCol, debitCol, creditCol *int
	if mapping.CategoryCol >= 0 {
		categoryCol = &mapping.CategoryCol
	}
	if mapping.IsDoubleEntry {
		debitCol = &mapping.DebitCol
		creditCol = &mapping.CreditCol
	} else {
		amountCol = &mapping.AmountCol
	}

	m := &repository.BankMapping{
		UserID:           &userID,
		Fingerprint:      fingerprint,
		BankName:         bankNamePtr,
		Delimiter:        ";", // Will be updated based on actual file
		SkipLines:        0,   // Will be updated based on actual file
		DateFormat:       mapping.DateFormat,
		DateCol:          mapping.DateCol,
		DescCol:          mapping.DescCol,
		CategoryCol:      categoryCol,
		AmountCol:        amountCol,
		DebitCol:         debitCol,
		CreditCol:        creditCol,
		IsEuropeanFormat: mapping.IsEuropeanFormat,
	}

	return s.repo.CreateMapping(ctx, m)
}

// ImportWithMapping processes a file using the provided column mapping
func (s *ImportService) ImportWithMapping(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID, fileData []byte, mapping ColumnMapping) (*ImportResult, error) {
	// Detect file config for delimiter and skip lines
	config, err := sniffer.DetectConfig(fileData)
	if err != nil {
		return nil, fmt.Errorf("failed to detect file config: %w", err)
	}

	// Create a file record
	fileRecord := &repository.UserFile{
		UserID:    userID,
		Type:      "csv",
		MimeType:  "text/csv",
		FileName:  "import.csv",
		SizeBytes: int64(len(fileData)),
	}
	if err := s.repo.CreateUserFile(ctx, fileRecord); err != nil {
		return nil, fmt.Errorf("failed to create file record: %w", err)
	}

	// Create import job
	job := &repository.ImportJob{
		UserID:    userID,
		FileID:    fileRecord.ID,
		Kind:      "transactions",
		Status:    "running",
		AccountID: accountID,
		RowsTotal: 0,
	}
	if err := s.repo.CreateImportJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create import job: %w", err)
	}

	parseCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results, preErrors := s.parseTransactionsStream(parseCtx, fileData, config, mapping)

	errors := make([]string, 0, len(preErrors))
	errors = append(errors, preErrors...)
	rowsFailed := len(preErrors)
	rowsImported := 0

	type parseError struct {
		lineNum int
		err     error
	}

	var parseErrors []parseError
	batch := make([]*repository.ParsedTransaction, 0, importBatchSize)
	progressSinceUpdate := rowsFailed

	updateProgress := func() {
		if err := s.repo.UpdateImportJobProgress(ctx, job.ID, rowsImported, rowsFailed); err != nil {
			s.logger.Warn("failed to update import job progress", "error", err)
		}
	}

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		imported, err := s.repo.BulkInsertTransactions(ctx, userID, accountID, batch)
		if err != nil {
			return err
		}
		rowsImported += imported
		batch = batch[:0]
		updateProgress()
		progressSinceUpdate = 0
		return nil
	}

	var insertErr error
	for result := range results {
		if insertErr != nil {
			continue
		}
		if result.err != nil {
			parseErrors = append(parseErrors, parseError{lineNum: result.lineNum, err: result.err})
			rowsFailed++
			progressSinceUpdate++
			if progressSinceUpdate >= importProgressUpdateEvery {
				updateProgress()
				progressSinceUpdate = 0
			}
			continue
		}

		batch = append(batch, result.tx)
		if len(batch) >= importBatchSize {
			if err := flushBatch(); err != nil {
				insertErr = err
				cancel()
			}
		}
	}

	if insertErr == nil {
		if err := flushBatch(); err != nil {
			insertErr = err
		}
	}

	if progressSinceUpdate > 0 && insertErr == nil {
		updateProgress()
	}

	if len(parseErrors) > 0 {
		sort.Slice(parseErrors, func(i, j int) bool {
			return parseErrors[i].lineNum < parseErrors[j].lineNum
		})
		for _, parseErr := range parseErrors {
			errors = append(errors, fmt.Sprintf("line %d: %v", parseErr.lineNum, parseErr.err))
		}
	}

	if insertErr != nil {
		errMsg := insertErr.Error()
		s.repo.FinishImportJob(ctx, job.ID, "failed", rowsImported, rowsFailed, &errMsg)
		return nil, fmt.Errorf("failed to insert transactions: %w", insertErr)
	}

	// Mark job as complete
	status := "succeeded"
	if err := s.repo.FinishImportJob(ctx, job.ID, status, rowsImported, rowsFailed, nil); err != nil {
		s.logger.Warn("failed to finish import job", "error", err)
	}

	return &ImportResult{
		JobID:        job.ID,
		RowsTotal:    rowsImported + rowsFailed,
		RowsImported: rowsImported,
		RowsFailed:   rowsFailed,
		Errors:       errors,
	}, nil
}

// ImportWithExistingMapping uses a pre-existing bank mapping to import
func (s *ImportService) ImportWithExistingMapping(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID, fileData []byte, mappingID uuid.UUID) (*ImportResult, error) {
	// This would fetch the mapping and call ImportWithMapping
	// For now, return an error indicating it's not implemented
	return nil, fmt.Errorf("import with existing mapping not yet implemented")
}

// parseTransactionsStream streams parsed rows from a CSV file.
func (s *ImportService) parseTransactionsStream(ctx context.Context, fileData []byte, config *sniffer.FileConfig, mapping ColumnMapping) (<-chan parseResult, []string) {
	results := make(chan parseResult, 1)

	reader := csv.NewReader(bytes.NewReader(fileData))
	reader.Comma = config.Delimiter
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	var errors []string
	for i := 0; i <= config.SkipLines; i++ {
		_, err := reader.Read()
		if err == io.EOF {
			close(results)
			return results, []string{"file has no data rows"}
		}
		if err != nil {
			errors = append(errors, fmt.Sprintf("error reading line %d: %v", i, err))
		}
	}

	workerCount := runtime.GOMAXPROCS(0)
	if workerCount < 1 {
		workerCount = 1
	}

	results = make(chan parseResult, workerCount*4)
	jobs := make(chan parseJob, workerCount*4)

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				tx, err := s.parseRow(job.record, mapping, job.lineNum)
				select {
				case results <- parseResult{lineNum: job.lineNum, tx: tx, err: err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		lineNum := config.SkipLines + 2 // 1-indexed, after header
		for {
			if ctx.Err() != nil {
				return
			}
			record, err := reader.Read()
			if err == io.EOF {
				return
			}
			if err != nil {
				select {
				case results <- parseResult{lineNum: lineNum, err: err}:
				case <-ctx.Done():
					return
				}
				lineNum++
				continue
			}
			select {
			case jobs <- parseJob{lineNum: lineNum, record: record}:
			case <-ctx.Done():
				return
			}
			lineNum++
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	return results, errors
}

// parseRow converts a CSV row into a ParsedTransaction
func (s *ImportService) parseRow(record []string, mapping ColumnMapping, lineNum int) (*repository.ParsedTransaction, error) {
	// Validate column indices
	maxCol := len(record) - 1
	if mapping.DateCol > maxCol || mapping.DescCol > maxCol {
		return nil, fmt.Errorf("column index out of bounds")
	}

	// Parse date
	dateStr := record[mapping.DateCol]
	date, err := normalizer.ParseFlexibleDate(dateStr, mapping.DateFormat, time.UTC)
	if err != nil {
		return nil, fmt.Errorf("invalid date '%s': %w", dateStr, err)
	}

	// Parse description
	description := normalizer.CleanDescription(record[mapping.DescCol])
	if description == "" {
		return nil, fmt.Errorf("empty description")
	}

	// Parse amount
	var amountCents int64
	if mapping.IsDoubleEntry {
		if mapping.DebitCol > maxCol || mapping.CreditCol > maxCol {
			return nil, fmt.Errorf("debit/credit column index out of bounds")
		}
		debitStr := ""
		creditStr := ""
		if mapping.DebitCol >= 0 && mapping.DebitCol < len(record) {
			debitStr = record[mapping.DebitCol]
		}
		if mapping.CreditCol >= 0 && mapping.CreditCol < len(record) {
			creditStr = record[mapping.CreditCol]
		}
		amountCents, err = normalizer.NormalizeDebitCredit(debitStr, creditStr, mapping.IsEuropeanFormat)
	} else {
		if mapping.AmountCol > maxCol {
			return nil, fmt.Errorf("amount column index out of bounds")
		}
		amountStr := record[mapping.AmountCol]
		amountCents, err = normalizer.ParseAmount(amountStr, mapping.IsEuropeanFormat)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	// Parse category (optional)
	var category string
	if mapping.CategoryCol >= 0 && mapping.CategoryCol < len(record) {
		category = normalizer.CleanDescription(record[mapping.CategoryCol])
	}

	return &repository.ParsedTransaction{
		Date:        date,
		Description: description,
		AmountCents: amountCents,
		Category:    category,
	}, nil
}
