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
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

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
	Location         *time.Location
	Delimiter        rune // Detected delimiter from AnalyzeCsvFile
	SkipLines        int  // Number of lines to skip before header
}

// AnalyzeResult contains the result of analyzing an uploaded file
type AnalyzeResult struct {
	// File analysis
	FileConfig        *sniffer.FileConfig
	ColumnSuggestions *sniffer.ColumnSuggestions
	ProbedDialect     *sniffer.RegionalDialect

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

// ImportOptions allows callers to override detected file settings.
type ImportOptions struct {
	HeaderRows      int
	Timezone        string
	InstitutionName string // Name of the bank/institution for this import
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
	normalizedData := normalizeCSVBytes(fileData)
	config, err := sniffer.DetectConfig(normalizedData)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze file: %w", err)
	}

	// Step 2: Get column suggestions
	suggestions := sniffer.SuggestColumns(config.Headers)

	// Step 3: Probe regional dialect from sample data
	amountIdx := suggestions.AmountCol
	if suggestions.IsDoubleEntry && amountIdx < 0 {
		// Use debit column for probing if double-entry
		amountIdx = suggestions.DebitCol
	}
	dialect := sniffer.ProbeDialect(config.SampleRows, amountIdx, suggestions.DateCol)

	// Step 4: Check for existing mapping
	mapping, err := s.repo.GetMappingByFingerprint(ctx, config.Fingerprint, &userID)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup mapping: %w", err)
	}

	result := &AnalyzeResult{
		FileConfig:        config,
		ColumnSuggestions: suggestions,
		ProbedDialect:     dialect,
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

// ImportWithMapping processes a file using the provided column mapping.
func (s *ImportService) ImportWithMapping(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID, fileData []byte, mapping ColumnMapping) (*ImportResult, error) {
	return s.ImportWithOptions(ctx, userID, accountID, fileData, mapping, ImportOptions{})
}

// ImportWithOptions processes a file using the provided column mapping and options.
func (s *ImportService) ImportWithOptions(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID, fileData []byte, mapping ColumnMapping, opts ImportOptions) (*ImportResult, error) {
	normalizedData := normalizeCSVBytes(fileData)

	detectOpts := &sniffer.DetectOptions{HeaderRowIndex: -1}

	// Use delimiter from mapping if provided (from AnalyzeCsvFile)
	if mapping.Delimiter != 0 {
		detectOpts.Delimiter = mapping.Delimiter
	}

	// Use skip lines from mapping if provided, otherwise use opts.HeaderRows
	if mapping.SkipLines > 0 {
		detectOpts.HeaderRowIndex = mapping.SkipLines
	} else if opts.HeaderRows > 0 {
		detectOpts.HeaderRowIndex = opts.HeaderRows - 1
	}

	// Detect file config for delimiter and skip lines
	config, err := sniffer.DetectConfigWithOptions(normalizedData, detectOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to detect file config: %w", err)
	}

	resolvedMapping, err := resolveMapping(config, mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve column mapping: %w", err)
	}

	applyFormatDefaults(config, &resolvedMapping)
	resolvedMapping.Location = resolveLocation(opts.Timezone)

	currencyCode, err := s.resolveCurrencyCode(ctx, userID, accountID, normalizedData, config)
	if err != nil {
		return nil, err
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

	results, preErrors := s.parseTransactionsStream(parseCtx, normalizedData, config, resolvedMapping)

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
		imported, err := s.repo.BulkInsertTransactions(ctx, userID, accountID, currencyCode, job.ID, opts.InstitutionName, batch)
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
func (s *ImportService) parseRow(record []string, mapping ColumnMapping, _ int) (*repository.ParsedTransaction, error) {
	// Validate column indices
	maxCol := len(record) - 1
	if mapping.DateCol > maxCol || mapping.DescCol > maxCol {
		return nil, fmt.Errorf("column index out of bounds")
	}

	// Parse date
	dateStr := record[mapping.DateCol]
	date, err := normalizer.ParseFlexibleDate(dateStr, mapping.DateFormat, mapping.Location)
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

func resolveMapping(config *sniffer.FileConfig, mapping ColumnMapping) (ColumnMapping, error) {
	suggestions := sniffer.SuggestColumns(config.Headers)
	resolved := mapping

	if resolved.DateCol < 0 {
		resolved.DateCol = suggestions.DateCol
	}
	if resolved.DescCol < 0 {
		resolved.DescCol = suggestions.DescCol
	}
	if resolved.CategoryCol < 0 && suggestions.CategoryCol >= 0 {
		resolved.CategoryCol = suggestions.CategoryCol
	}

	if resolved.IsDoubleEntry || resolved.DebitCol >= 0 || resolved.CreditCol >= 0 {
		if resolved.DebitCol < 0 {
			resolved.DebitCol = suggestions.DebitCol
		}
		if resolved.CreditCol < 0 {
			resolved.CreditCol = suggestions.CreditCol
		}
		resolved.IsDoubleEntry = true
	} else if resolved.AmountCol < 0 {
		if suggestions.AmountCol >= 0 {
			resolved.AmountCol = suggestions.AmountCol
		} else if suggestions.IsDoubleEntry {
			resolved.DebitCol = suggestions.DebitCol
			resolved.CreditCol = suggestions.CreditCol
			resolved.IsDoubleEntry = true
		}
	}

	if resolved.DateCol < 0 || resolved.DescCol < 0 {
		return resolved, fmt.Errorf("missing required columns for date/description")
	}

	if resolved.IsDoubleEntry {
		if resolved.DebitCol < 0 || resolved.CreditCol < 0 {
			return resolved, fmt.Errorf("missing required debit/credit columns")
		}
	} else if resolved.AmountCol < 0 {
		return resolved, fmt.Errorf("missing required amount column")
	}

	maxHeaderCol := len(config.Headers) - 1
	if maxHeaderCol >= 0 {
		if resolved.DateCol > maxHeaderCol || resolved.DescCol > maxHeaderCol {
			return resolved, fmt.Errorf("column index out of bounds for detected headers")
		}
		if resolved.IsDoubleEntry {
			if resolved.DebitCol > maxHeaderCol || resolved.CreditCol > maxHeaderCol {
				return resolved, fmt.Errorf("debit/credit column index out of bounds for detected headers")
			}
		} else if resolved.AmountCol > maxHeaderCol {
			return resolved, fmt.Errorf("amount column index out of bounds for detected headers")
		}
	}

	return resolved, nil
}

func applyFormatDefaults(config *sniffer.FileConfig, mapping *ColumnMapping) {
	if mapping.DateFormat == "" {
		dateSamples := collectSamples(config.SampleRows, mapping.DateCol)
		if len(dateSamples) > 0 {
			mapping.DateFormat = normalizer.DetectDateFormat(dateSamples)
		}
	}

	if european, ok := detectEuropeanFormat(config.SampleRows, *mapping); ok {
		mapping.IsEuropeanFormat = european
	} else if config.Delimiter == ';' {
		mapping.IsEuropeanFormat = true
	} else if config.Delimiter == ',' {
		mapping.IsEuropeanFormat = false
	}
}

func resolveLocation(timezone string) *time.Location {
	if timezone == "" {
		return nil
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil
	}
	return loc
}

func (s *ImportService) resolveCurrencyCode(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID, data []byte, config *sniffer.FileConfig) (string, error) {
	if accountID != nil {
		currency, err := s.repo.GetAccountCurrency(ctx, userID, *accountID)
		if err != nil {
			return "", fmt.Errorf("failed to resolve account currency: %w", err)
		}
		if currency == "" {
			return "", fmt.Errorf("account currency not found; invalid account_id")
		}
		if code, ok := normalizeCurrencyCode(currency); ok {
			return code, nil
		}
		return "", fmt.Errorf("invalid account currency code: %s", currency)
	}

	if code, ok := detectCurrencyFromFile(data, config); ok {
		return code, nil
	}

	return "", fmt.Errorf("currency code not found; provide account_id or include currency in CSV")
}

func normalizeCSVBytes(data []byte) []byte {
	data = stripUTF8BOM(data)
	if utf8.Valid(data) {
		return data
	}
	return decodeLatin1(data)
}

func stripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func decodeLatin1(data []byte) []byte {
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(b)
	}
	return []byte(string(runes))
}

func detectEuropeanFormat(sampleRows [][]string, mapping ColumnMapping) (bool, bool) {
	samples := collectAmountSamples(sampleRows, mapping)
	return detectEuropeanFormatSamples(samples)
}

func collectSamples(rows [][]string, col int) []string {
	if col < 0 {
		return nil
	}
	samples := make([]string, 0, len(rows))
	for _, row := range rows {
		if col >= 0 && col < len(row) {
			if value := strings.TrimSpace(row[col]); value != "" {
				samples = append(samples, value)
			}
		}
	}
	return samples
}

func collectAmountSamples(rows [][]string, mapping ColumnMapping) []string {
	if mapping.IsDoubleEntry {
		samples := collectSamples(rows, mapping.DebitCol)
		samples = append(samples, collectSamples(rows, mapping.CreditCol)...)
		return samples
	}
	return collectSamples(rows, mapping.AmountCol)
}

func detectEuropeanFormatSamples(samples []string) (bool, bool) {
	europeanHints := 0
	usHints := 0

	for _, raw := range samples {
		cleaned := cleanAmountSample(raw)
		cleaned = strings.TrimPrefix(cleaned, "-")
		if cleaned == "" {
			continue
		}

		hasComma := strings.Contains(cleaned, ",")
		hasDot := strings.Contains(cleaned, ".")

		switch {
		case hasComma && hasDot:
			if strings.LastIndex(cleaned, ",") > strings.LastIndex(cleaned, ".") {
				europeanHints++
			} else {
				usHints++
			}
		case hasComma:
			if hasDecimalSuffix(cleaned, ',') {
				europeanHints++
			}
		case hasDot:
			if hasDecimalSuffix(cleaned, '.') {
				usHints++
			}
		}
	}

	if europeanHints == 0 && usHints == 0 {
		return false, false
	}
	if europeanHints == usHints {
		return false, false
	}
	return europeanHints > usHints, true
}

func detectCurrencyFromFile(data []byte, config *sniffer.FileConfig) (string, bool) {
	lines := strings.Split(string(data), "\n")
	maxLine := config.SkipLines
	if maxLine > len(lines) {
		maxLine = len(lines)
	}

	for i := 0; i < maxLine; i++ {
		line := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		if line == "" {
			continue
		}
		if code, ok := detectCurrencyFromLine(line, true); ok {
			return code, true
		}
	}

	if idx := currencyColumnIndex(config.Headers); idx >= 0 {
		for _, row := range config.SampleRows {
			if idx >= len(row) {
				continue
			}
			value := strings.TrimSpace(row[idx])
			if value == "" {
				continue
			}
			if code, ok := normalizeCurrencyCode(value); ok {
				return code, true
			}
			if code, ok := detectCurrencyFromSymbols(value); ok {
				return code, true
			}
		}
	}

	return "", false
}

func detectCurrencyFromLine(line string, allowLoose bool) (string, bool) {
	if code, ok := detectCurrencyFromSymbols(line); ok {
		return code, true
	}

	lower := strings.ToLower(line)
	if containsCurrencyKeyword(lower) {
		if code, ok := normalizeCurrencyCode(line); ok {
			return code, true
		}
	}

	if allowLoose && strings.Contains(line, "-") {
		if code, ok := extractSingleCurrencyToken(line); ok {
			return code, true
		}
	}

	return "", false
}

func currencyColumnIndex(headers []string) int {
	for i, header := range headers {
		h := strings.ToLower(strings.TrimSpace(header))
		if h == "" {
			continue
		}
		if strings.Contains(h, "currency") || strings.Contains(h, "moeda") ||
			strings.Contains(h, "moneda") || strings.Contains(h, "divisa") ||
			strings.Contains(h, "devise") || strings.Contains(h, "valuta") {
			return i
		}
	}
	return -1
}

func normalizeCurrencyCode(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	cleaned := strings.Trim(strings.TrimSpace(value), "\"'")
	if cleaned == "" {
		return "", false
	}
	cleaned = strings.ToUpper(cleaned)
	if isCurrencyCode(cleaned) {
		return cleaned, true
	}
	return extractSingleCurrencyToken(cleaned)
}

func extractSingleCurrencyToken(value string) (string, bool) {
	tokens := extractCurrencyTokens(value)
	if len(tokens) != 1 {
		return "", false
	}
	return tokens[0], true
}

func extractCurrencyTokens(value string) []string {
	upper := strings.ToUpper(value)
	tokens := strings.FieldsFunc(upper, func(r rune) bool {
		switch r {
		case ';', ',', '\t', '|', '-', ':', '/', '(', ')':
			return true
		}
		return unicode.IsSpace(r)
	})

	codes := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.Trim(token, "\"'")
		if isCurrencyCode(token) {
			codes = append(codes, token)
		}
	}
	return codes
}

func isCurrencyCode(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, r := range value {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func containsCurrencyKeyword(lower string) bool {
	return strings.Contains(lower, "currency") ||
		strings.Contains(lower, "moeda") ||
		strings.Contains(lower, "moneda") ||
		strings.Contains(lower, "divisa") ||
		strings.Contains(lower, "devise") ||
		strings.Contains(lower, "valuta")
}

func detectCurrencyFromSymbols(value string) (string, bool) {
	switch {
	case strings.Contains(value, "\u20ac"):
		return "EUR", true
	case strings.Contains(value, "\u00a3"):
		return "GBP", true
	case strings.Contains(value, "\u00a5") || strings.Contains(value, "\uffe5"):
		return "JPY", true
	case strings.Contains(value, "\u20b9"):
		return "INR", true
	case strings.Contains(value, "\u20bd"):
		return "RUB", true
	case strings.Contains(value, "\u20a9"):
		return "KRW", true
	case strings.Contains(value, "\u20ba"):
		return "TRY", true
	case strings.Contains(value, "\u20ab"):
		return "VND", true
	case strings.Contains(value, "\u20aa"):
		return "ILS", true
	case strings.Contains(value, "$"):
		return "USD", true
	}
	return "", false
}

func cleanAmountSample(raw string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) || r == ',' || r == '.' || r == '-' {
			return r
		}
		return -1
	}, raw)
}

func hasDecimalSuffix(value string, sep rune) bool {
	idx := strings.LastIndex(value, string(sep))
	if idx == -1 || idx == len(value)-1 {
		return false
	}
	digits := 0
	for _, r := range value[idx+1:] {
		if !unicode.IsDigit(r) {
			return false
		}
		digits++
		if digits > 2 {
			return false
		}
	}
	return digits > 0
}
