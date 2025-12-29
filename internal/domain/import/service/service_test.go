package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/import/repository"
	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/import/sniffer"
	"github.com/google/uuid"
)

func TestParseTransactions_OrderAndErrors(t *testing.T) {
	data := strings.Join([]string{
		"Date,Description,Amount,Category",
		"13/02/2024,Store A,10.50,Food",
		"invalid-date,Store B,5.00,Food",
		"14/02/2024,Store C,12.00,Food",
		"15/02/2024,Store D,not-amount,Food",
		"16/02/2024,Store E,-3.25,Food",
		"",
	}, "\n")

	config, err := sniffer.DetectConfig([]byte(data))
	if err != nil {
		t.Fatalf("DetectConfig failed: %v", err)
	}

	mapping := ColumnMapping{
		DateCol:          0,
		DescCol:          1,
		CategoryCol:      3,
		AmountCol:        2,
		IsDoubleEntry:    false,
		IsEuropeanFormat: false,
	}

	svc := &ImportService{}
	results, preErrors := svc.parseTransactionsStream(context.Background(), []byte(data), config, mapping)
	if len(preErrors) != 0 {
		t.Fatalf("unexpected pre-parse errors: %v", preErrors)
	}
	transactions, parseErrors := collectParseResults(results)

	if len(transactions) != 3 {
		t.Fatalf("expected 3 transactions, got %d", len(transactions))
	}

	gotAmounts := make(map[string]int64)
	for _, tx := range transactions {
		gotAmounts[tx.Description] = tx.AmountCents
	}
	expected := map[string]int64{
		"Store A": 1050,
		"Store C": 1200,
		"Store E": -325,
	}
	if len(gotAmounts) != len(expected) {
		t.Fatalf("unexpected transaction count: got %d, want %d", len(gotAmounts), len(expected))
	}
	for desc, wantAmount := range expected {
		gotAmount, ok := gotAmounts[desc]
		if !ok {
			t.Fatalf("missing transaction for %s", desc)
		}
		if gotAmount != wantAmount {
			t.Fatalf("unexpected amount for %s: got %d, want %d", desc, gotAmount, wantAmount)
		}
	}

	if len(parseErrors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(parseErrors))
	}
	sort.Slice(parseErrors, func(i, j int) bool {
		return parseErrors[i].lineNum < parseErrors[j].lineNum
	})
	if parseErrors[0].lineNum != 3 || !strings.Contains(parseErrors[0].err.Error(), "invalid date") {
		t.Fatalf("unexpected first error: line %d %v", parseErrors[0].lineNum, parseErrors[0].err)
	}
	if parseErrors[1].lineNum != 5 || !strings.Contains(parseErrors[1].err.Error(), "invalid amount") {
		t.Fatalf("unexpected second error: line %d %v", parseErrors[1].lineNum, parseErrors[1].err)
	}
}

func TestParseTransactions_DoubleEntryWithSkipLines(t *testing.T) {
	data := strings.Join([]string{
		"Account;123",
		"Period;Jan",
		"Date;Description;Debit;Credit;Category",
		"02-01-2024;Coffee;2,50;;Food",
		"03-01-2024;Salary;;1000,00;Income",
		"",
	}, "\n")

	config, err := sniffer.DetectConfig([]byte(data))
	if err != nil {
		t.Fatalf("DetectConfig failed: %v", err)
	}
	if config.SkipLines != 2 {
		t.Fatalf("expected 2 skip lines, got %d", config.SkipLines)
	}

	mapping := ColumnMapping{
		DateCol:          0,
		DescCol:          1,
		DebitCol:         2,
		CreditCol:        3,
		CategoryCol:      4,
		IsDoubleEntry:    true,
		IsEuropeanFormat: true,
	}

	svc := &ImportService{}
	results, preErrors := svc.parseTransactionsStream(context.Background(), []byte(data), config, mapping)
	if len(preErrors) != 0 {
		t.Fatalf("unexpected pre-parse errors: %v", preErrors)
	}
	transactions, parseErrors := collectParseResults(results)

	if len(parseErrors) != 0 {
		t.Fatalf("expected no errors, got %v", parseErrors)
	}
	if len(transactions) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(transactions))
	}

	gotAmounts := make(map[string]int64)
	for _, tx := range transactions {
		gotAmounts[tx.Description] = tx.AmountCents
	}
	if gotAmounts["Coffee"] != -250 {
		t.Fatalf("unexpected Coffee amount: %d", gotAmounts["Coffee"])
	}
	if gotAmounts["Salary"] != 100000 {
		t.Fatalf("unexpected Salary amount: %d", gotAmounts["Salary"])
	}
}

func TestImportWithMapping_BatchesAndProgress(t *testing.T) {
	rows := importBatchSize + 5
	var builder strings.Builder
	builder.Grow(rows * 48)
	builder.WriteString("Date,Description,Amount,Category\n")
	for i := 0; i < rows; i++ {
		builder.WriteString(fmt.Sprintf("13/02/2024,Merchant %d,1.00,Food\n", i))
	}

	mapping := ColumnMapping{
		DateCol:          0,
		DescCol:          1,
		CategoryCol:      3,
		AmountCol:        2,
		IsDoubleEntry:    false,
		IsEuropeanFormat: false,
	}

	repo := &fakeImportRepo{accountCurrency: "USD"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewImportService(repo, logger)

	accountID := uuid.New()
	result, err := svc.ImportWithMapping(context.Background(), uuid.New(), &accountID, []byte(builder.String()), mapping)
	if err != nil {
		t.Fatalf("ImportWithMapping failed: %v", err)
	}
	if result.RowsImported != rows {
		t.Fatalf("expected %d imported rows, got %d", rows, result.RowsImported)
	}
	if result.RowsFailed != 0 {
		t.Fatalf("expected 0 failed rows, got %d", result.RowsFailed)
	}

	bulkSizes := repo.bulkSizes()
	if len(bulkSizes) != 2 {
		t.Fatalf("expected 2 bulk inserts, got %d", len(bulkSizes))
	}
	if bulkSizes[0] != importBatchSize {
		t.Fatalf("expected first batch size %d, got %d", importBatchSize, bulkSizes[0])
	}
	if bulkSizes[1] != rows-importBatchSize {
		t.Fatalf("expected second batch size %d, got %d", rows-importBatchSize, bulkSizes[1])
	}

	progress := repo.progressCalls()
	if len(progress) != 2 {
		t.Fatalf("expected 2 progress updates, got %d", len(progress))
	}
	if progress[0].rowsImported != importBatchSize || progress[0].rowsFailed != 0 {
		t.Fatalf("unexpected first progress update: %+v", progress[0])
	}
	if progress[1].rowsImported != rows || progress[1].rowsFailed != 0 {
		t.Fatalf("unexpected second progress update: %+v", progress[1])
	}
}

func BenchmarkParseTransactionsSequential(b *testing.B) {
	data, config, mapping := benchmarkCSVFixture(5000)
	svc := &ImportService{}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		transactions, parseErrors := parseTransactionsSequential(svc, data, config, mapping)
		benchmarkSink = len(transactions) + len(parseErrors)
	}
}

func BenchmarkParseTransactionsConcurrent(b *testing.B) {
	data, config, mapping := benchmarkCSVFixture(5000)
	svc := &ImportService{}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		results, preErrors := svc.parseTransactionsStream(context.Background(), data, config, mapping)
		txCount := 0
		errCount := len(preErrors)
		for result := range results {
			if result.err != nil {
				errCount++
				continue
			}
			txCount++
		}
		benchmarkSink = txCount + errCount
	}
}

var benchmarkSink int

func benchmarkCSVFixture(rows int) ([]byte, *sniffer.FileConfig, ColumnMapping) {
	var builder strings.Builder
	builder.Grow(rows * 64)
	builder.WriteString("Date,Description,Amount,Category\n")
	for i := 0; i < rows; i++ {
		builder.WriteString(fmt.Sprintf("13/02/2024,Merchant %d,%d.%02d,Category\n", i, i%200, i%100))
	}

	data := []byte(builder.String())
	config, err := sniffer.DetectConfig(data)
	if err != nil {
		panic(err)
	}

	mapping := ColumnMapping{
		DateCol:          0,
		DescCol:          1,
		CategoryCol:      3,
		AmountCol:        2,
		IsDoubleEntry:    false,
		IsEuropeanFormat: false,
	}

	return data, config, mapping
}

func parseTransactionsSequential(s *ImportService, fileData []byte, config *sniffer.FileConfig, mapping ColumnMapping) ([]*repository.ParsedTransaction, []string) {
	var transactions []*repository.ParsedTransaction
	var errors []string

	reader := csv.NewReader(bytes.NewReader(fileData))
	reader.Comma = config.Delimiter
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	for i := 0; i <= config.SkipLines; i++ {
		_, err := reader.Read()
		if err == io.EOF {
			return nil, []string{"file has no data rows"}
		}
		if err != nil {
			errors = append(errors, fmt.Sprintf("error reading line %d: %v", i, err))
		}
	}

	lineNum := config.SkipLines + 2
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			errors = append(errors, fmt.Sprintf("line %d: %v", lineNum, err))
			lineNum++
			continue
		}

		tx, err := s.parseRow(record, mapping, lineNum)
		if err != nil {
			errors = append(errors, fmt.Sprintf("line %d: %v", lineNum, err))
			lineNum++
			continue
		}

		transactions = append(transactions, tx)
		lineNum++
	}

	return transactions, errors
}

type parseError struct {
	lineNum int
	err     error
}

func collectParseResults(results <-chan parseResult) ([]*repository.ParsedTransaction, []parseError) {
	var transactions []*repository.ParsedTransaction
	var parseErrors []parseError

	for result := range results {
		if result.err != nil {
			parseErrors = append(parseErrors, parseError{lineNum: result.lineNum, err: result.err})
			continue
		}
		if result.tx != nil {
			transactions = append(transactions, result.tx)
		}
	}

	return transactions, parseErrors
}

type progressSnapshot struct {
	rowsImported int
	rowsFailed   int
}

type fakeImportRepo struct {
	mu                sync.Mutex
	bulkInserts       []int
	progressSnapshots []progressSnapshot
	accountCurrency   string
}

func (f *fakeImportRepo) GetMappingByFingerprint(ctx context.Context, fingerprint string, userID *uuid.UUID) (*repository.BankMapping, error) {
	return nil, nil
}

func (f *fakeImportRepo) CreateMapping(ctx context.Context, mapping *repository.BankMapping) error {
	return nil
}

func (f *fakeImportRepo) UpdateMapping(ctx context.Context, mapping *repository.BankMapping) error {
	return nil
}

func (f *fakeImportRepo) ListUserMappings(ctx context.Context, userID uuid.UUID) ([]*repository.BankMapping, error) {
	return nil, nil
}

func (f *fakeImportRepo) GetAccountCurrency(ctx context.Context, userID uuid.UUID, accountID uuid.UUID) (string, error) {
	return f.accountCurrency, nil
}

func (f *fakeImportRepo) CreateUserFile(ctx context.Context, file *repository.UserFile) error {
	if file.ID == uuid.Nil {
		file.ID = uuid.New()
	}
	return nil
}

func (f *fakeImportRepo) GetUserFileByID(ctx context.Context, id uuid.UUID) (*repository.UserFile, error) {
	return nil, nil
}

func (f *fakeImportRepo) CreateImportJob(ctx context.Context, job *repository.ImportJob) error {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	return nil
}

func (f *fakeImportRepo) GetImportJobByID(ctx context.Context, id uuid.UUID) (*repository.ImportJob, error) {
	return nil, nil
}

func (f *fakeImportRepo) UpdateImportJobProgress(ctx context.Context, id uuid.UUID, rowsImported, rowsFailed int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.progressSnapshots = append(f.progressSnapshots, progressSnapshot{
		rowsImported: rowsImported,
		rowsFailed:   rowsFailed,
	})
	return nil
}

func (f *fakeImportRepo) UpdateImportJobStatus(ctx context.Context, id uuid.UUID, status string, errorMessage *string) error {
	return nil
}

func (f *fakeImportRepo) FinishImportJob(ctx context.Context, id uuid.UUID, status string, rowsImported, rowsFailed int, errorMessage *string) error {
	return nil
}

func (f *fakeImportRepo) BulkInsertTransactions(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID, currencyCode string, importJobID uuid.UUID, institutionName string, txs []*repository.ParsedTransaction) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bulkInserts = append(f.bulkInserts, len(txs))
	return len(txs), nil
}

func (f *fakeImportRepo) ListTransactions(ctx context.Context, userID uuid.UUID, filter repository.ListTransactionsFilter) ([]*repository.Transaction, int64, error) {
	return nil, 0, nil
}

func (f *fakeImportRepo) DeleteByImportJobID(ctx context.Context, userID uuid.UUID, importJobID uuid.UUID) (int, error) {
	return 0, nil
}

func (f *fakeImportRepo) bulkSizes() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	sizes := make([]int, len(f.bulkInserts))
	copy(sizes, f.bulkInserts)
	return sizes
}

func (f *fakeImportRepo) progressCalls() []progressSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := make([]progressSnapshot, len(f.progressSnapshots))
	copy(calls, f.progressSnapshots)
	return calls
}

// ============================================================================
// Real Bank File Tests
// ============================================================================

// TestImportRealFile_Revolut tests importing a real Revolut CSV file
func TestImportRealFile_Revolut(t *testing.T) {
	data, err := loadTestFile("../../../data/import/account-statement_2019-12-01_2025-12-28_en_e1631a.csv")
	if err != nil {
		t.Skipf("Skipping real file test: %v", err)
	}

	config, err := sniffer.DetectConfig(data)
	if err != nil {
		t.Fatalf("DetectConfig failed: %v", err)
	}

	// Verify detection
	if config.Delimiter != ',' {
		t.Errorf("expected comma delimiter, got %c", config.Delimiter)
	}
	if len(config.Headers) < 8 {
		t.Errorf("expected at least 8 headers, got %d: %v", len(config.Headers), config.Headers)
	}

	// Build mapping from detected suggestions
	suggestions := sniffer.SuggestColumns(config.Headers)

	// Revolut has: Type,Product,Started Date,Completed Date,Description,Amount,Fee,Currency,State,Balance
	// Date is column 2 (Started Date), Description is 4, Amount is 5
	mapping := ColumnMapping{
		DateCol:          2, // Started Date
		DescCol:          4, // Description
		AmountCol:        5, // Amount
		CategoryCol:      -1,
		IsDoubleEntry:    suggestions.IsDoubleEntry,
		IsEuropeanFormat: false, // Revolut uses US number format
		DateFormat:       "YYYY-MM-DD HH:mm:ss",
	}

	svc := &ImportService{}
	results, preErrors := svc.parseTransactionsStream(context.Background(), data, config, mapping)
	if len(preErrors) != 0 {
		t.Logf("pre-parse warnings: %v", preErrors)
	}

	transactions, parseErrors := collectParseResults(results)

	t.Logf("Revolut: Parsed %d transactions, %d errors", len(transactions), len(parseErrors))

	if len(transactions) == 0 {
		t.Errorf("expected some transactions, got 0")
	}

	// Sample check - should have some positive and negative amounts
	hasPositive := false
	hasNegative := false
	for _, tx := range transactions {
		if tx.AmountCents > 0 {
			hasPositive = true
		}
		if tx.AmountCents < 0 {
			hasNegative = true
		}
	}
	if !hasPositive || !hasNegative {
		t.Errorf("expected both positive and negative amounts")
	}
}

// TestImportRealFile_CaixaPortuguese tests importing a real Portuguese bank CSV file
func TestImportRealFile_CaixaPortuguese(t *testing.T) {
	data, err := loadTestFile("../../../data/import/comprovativo.csv")
	if err != nil {
		t.Skipf("Skipping real file test: %v", err)
	}

	// Normalize encoding (Portuguese files often use Latin-1)
	data = normalizeCSVBytes(data)

	config, err := sniffer.DetectConfig(data)
	if err != nil {
		t.Fatalf("DetectConfig failed: %v", err)
	}

	// Verify detection
	if config.Delimiter != ';' {
		t.Errorf("expected semicolon delimiter, got %c", config.Delimiter)
	}
	if config.SkipLines < 5 {
		t.Errorf("expected at least 5 skip lines (metadata), got %d", config.SkipLines)
	}

	// Build mapping from detected suggestions
	suggestions := sniffer.SuggestColumns(config.Headers)

	// Caixa has: Data mov. ;Data valor ;Descrição ;Débito ;Crédito ;...
	mapping := ColumnMapping{
		DateCol:          suggestions.DateCol,
		DescCol:          suggestions.DescCol,
		DebitCol:         suggestions.DebitCol,
		CreditCol:        suggestions.CreditCol,
		CategoryCol:      suggestions.CategoryCol,
		IsDoubleEntry:    true,
		IsEuropeanFormat: true, // Portuguese uses comma as decimal
		DateFormat:       "DD-MM-YYYY",
	}

	if mapping.DateCol < 0 {
		t.Fatalf("could not detect date column, headers: %v", config.Headers)
	}
	if mapping.DescCol < 0 {
		t.Fatalf("could not detect description column, headers: %v", config.Headers)
	}

	svc := &ImportService{}
	results, preErrors := svc.parseTransactionsStream(context.Background(), data, config, mapping)
	if len(preErrors) != 0 {
		t.Logf("pre-parse warnings: %v", preErrors)
	}

	transactions, parseErrors := collectParseResults(results)

	t.Logf("Caixa: Parsed %d transactions, %d errors", len(transactions), len(parseErrors))
	if len(parseErrors) > 0 && len(parseErrors) < 10 {
		for _, pe := range parseErrors {
			t.Logf("  Line %d: %v", pe.lineNum, pe.err)
		}
	}

	if len(transactions) == 0 {
		t.Errorf("expected some transactions, got 0")
	}

	// Sample check - should have both debits (negative) and credits (positive)
	hasDebit := false
	hasCredit := false
	for _, tx := range transactions {
		if tx.AmountCents < 0 {
			hasDebit = true
		}
		if tx.AmountCents > 0 {
			hasCredit = true
		}
	}
	if !hasDebit || !hasCredit {
		t.Errorf("expected both debit and credit transactions")
	}
}

// ============================================================================
// Real Bank File Benchmarks
// ============================================================================

func BenchmarkParseTransactionsSequential_Revolut(b *testing.B) {
	data, config, mapping := loadRevolutFixture(b)
	svc := &ImportService{}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		transactions, parseErrors := parseTransactionsSequential(svc, data, config, mapping)
		benchmarkSink = len(transactions) + len(parseErrors)
	}
}

func BenchmarkParseTransactionsConcurrent_Revolut(b *testing.B) {
	data, config, mapping := loadRevolutFixture(b)
	svc := &ImportService{}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		results, preErrors := svc.parseTransactionsStream(context.Background(), data, config, mapping)
		txCount := 0
		errCount := len(preErrors)
		for result := range results {
			if result.err != nil {
				errCount++
				continue
			}
			txCount++
		}
		benchmarkSink = txCount + errCount
	}
}

func BenchmarkParseTransactionsSequential_Caixa(b *testing.B) {
	data, config, mapping := loadCaixaFixture(b)
	svc := &ImportService{}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		transactions, parseErrors := parseTransactionsSequential(svc, data, config, mapping)
		benchmarkSink = len(transactions) + len(parseErrors)
	}
}

func BenchmarkParseTransactionsConcurrent_Caixa(b *testing.B) {
	data, config, mapping := loadCaixaFixture(b)
	svc := &ImportService{}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		results, preErrors := svc.parseTransactionsStream(context.Background(), data, config, mapping)
		txCount := 0
		errCount := len(preErrors)
		for result := range results {
			if result.err != nil {
				errCount++
				continue
			}
			txCount++
		}
		benchmarkSink = txCount + errCount
	}
}

// ============================================================================
// Helper functions for real file tests
// ============================================================================

func loadTestFile(relativePath string) ([]byte, error) {
	return os.ReadFile(relativePath)
}

func loadRevolutFixture(b *testing.B) ([]byte, *sniffer.FileConfig, ColumnMapping) {
	b.Helper()
	data, err := loadTestFile("../../../data/import/account-statement_2019-12-01_2025-12-28_en_e1631a.csv")
	if err != nil {
		b.Skipf("Skipping benchmark: %v", err)
	}

	config, err := sniffer.DetectConfig(data)
	if err != nil {
		b.Fatalf("DetectConfig failed: %v", err)
	}

	mapping := ColumnMapping{
		DateCol:          2, // Started Date
		DescCol:          4, // Description
		AmountCol:        5, // Amount
		CategoryCol:      -1,
		IsDoubleEntry:    false,
		IsEuropeanFormat: false,
		DateFormat:       "YYYY-MM-DD HH:mm:ss",
	}

	return data, config, mapping
}

func loadCaixaFixture(b *testing.B) ([]byte, *sniffer.FileConfig, ColumnMapping) {
	b.Helper()
	data, err := loadTestFile("../../../data/import/comprovativo.csv")
	if err != nil {
		b.Skipf("Skipping benchmark: %v", err)
	}

	data = normalizeCSVBytes(data)

	config, err := sniffer.DetectConfig(data)
	if err != nil {
		b.Fatalf("DetectConfig failed: %v", err)
	}

	suggestions := sniffer.SuggestColumns(config.Headers)

	mapping := ColumnMapping{
		DateCol:          suggestions.DateCol,
		DescCol:          suggestions.DescCol,
		DebitCol:         suggestions.DebitCol,
		CreditCol:        suggestions.CreditCol,
		CategoryCol:      suggestions.CategoryCol,
		IsDoubleEntry:    true,
		IsEuropeanFormat: true,
		DateFormat:       "DD-MM-YYYY",
	}

	return data, config, mapping
}
