package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
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

	repo := &fakeImportRepo{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewImportService(repo, logger)

	result, err := svc.ImportWithMapping(context.Background(), uuid.New(), nil, []byte(builder.String()), mapping)
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
	mu            sync.Mutex
	bulkInserts   []int
	progressCalls []progressSnapshot
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
	f.progressCalls = append(f.progressCalls, progressSnapshot{
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

func (f *fakeImportRepo) BulkInsertTransactions(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID, txs []*repository.ParsedTransaction) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bulkInserts = append(f.bulkInserts, len(txs))
	return len(txs), nil
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
	calls := make([]progressSnapshot, len(f.progressCalls))
	copy(calls, f.progressCalls)
	return calls
}
