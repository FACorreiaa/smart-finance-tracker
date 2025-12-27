package sniffer

import (
	"strings"
	"testing"
)

// Sample Portuguese bank CSV (CGD-style)
const samplePortugueseCSV = `Conta;12345678901
Data de início;01-01-2024
Data de fim;31-01-2024
Moeda;EUR
Saldo inicial;1000,00
Saldo final;850,00
Data mov.;Data valor;Descrição;Débito;Crédito;Saldo contabilístico;Saldo disponível;Categoria
02-01-2024;02-01-2024;Compra MB - Pingo Doce;45,23;;954,77;954,77;Alimentação
03-01-2024;03-01-2024;Netflix;12,99;;941,78;941,78;Entretenimento
05-01-2024;05-01-2024;Transferência recebida;;500,00;1441,78;1441,78;Transferências
`

// Sample American bank CSV
const sampleAmericanCSV = `Date,Description,Amount,Category
01/02/2024,Starbucks,-5.40,Food & Dining
01/03/2024,Amazon,-29.99,Shopping
01/05/2024,Payroll,2500.00,Income
`

// Sample TSV file
const sampleTSV = `Data mov.	Data valor	Descrição	Débito	Crédito	Saldo
02-01-2024	02-01-2024	Pingo Doce	45,23		954,77
03-01-2024	03-01-2024	Netflix	12,99		941,78
`

func TestDetectConfig_PortugueseCSV(t *testing.T) {
	config, err := DetectConfig([]byte(samplePortugueseCSV))
	if err != nil {
		t.Fatalf("DetectConfig failed: %v", err)
	}

	// Check delimiter
	if config.Delimiter != ';' {
		t.Errorf("Expected delimiter ';', got '%c'", config.Delimiter)
	}

	// Check skip lines (6 lines of metadata)
	if config.SkipLines != 6 {
		t.Errorf("Expected 6 skip lines, got %d", config.SkipLines)
	}

	// Check headers
	expectedHeaders := []string{"Data mov.", "Data valor", "Descrição", "Débito", "Crédito", "Saldo contabilístico", "Saldo disponível", "Categoria"}
	if len(config.Headers) != len(expectedHeaders) {
		t.Errorf("Expected %d headers, got %d", len(expectedHeaders), len(config.Headers))
	}

	// Check fingerprint is generated
	if config.Fingerprint == "" {
		t.Error("Expected non-empty fingerprint")
	}

	// Check sample rows
	if len(config.SampleRows) != 3 {
		t.Errorf("Expected 3 sample rows, got %d", len(config.SampleRows))
	}
}

func TestDetectConfig_AmericanCSV(t *testing.T) {
	config, err := DetectConfig([]byte(sampleAmericanCSV))
	if err != nil {
		t.Fatalf("DetectConfig failed: %v", err)
	}

	// Check delimiter
	if config.Delimiter != ',' {
		t.Errorf("Expected delimiter ',', got '%c'", config.Delimiter)
	}

	// Check skip lines (headers on first line)
	if config.SkipLines != 0 {
		t.Errorf("Expected 0 skip lines, got %d", config.SkipLines)
	}

	// Check headers
	if len(config.Headers) != 4 {
		t.Errorf("Expected 4 headers, got %d", len(config.Headers))
	}
}

func TestDetectConfig_TSV(t *testing.T) {
	config, err := DetectConfig([]byte(sampleTSV))
	if err != nil {
		t.Fatalf("DetectConfig failed: %v", err)
	}

	// Check delimiter
	if config.Delimiter != '\t' {
		t.Errorf("Expected tab delimiter, got '%c'", config.Delimiter)
	}
}

func TestDetectConfig_EmptyFile(t *testing.T) {
	_, err := DetectConfig([]byte{})
	if err != ErrEmptyFile {
		t.Errorf("Expected ErrEmptyFile, got %v", err)
	}
}

func TestSuggestColumns_Portuguese(t *testing.T) {
	headers := []string{"Data mov.", "Data valor", "Descrição", "Débito", "Crédito", "Saldo", "Categoria"}

	suggestions := SuggestColumns(headers)

	if suggestions.DateCol != 0 {
		t.Errorf("Expected date column 0, got %d", suggestions.DateCol)
	}

	if suggestions.DescCol != 2 {
		t.Errorf("Expected description column 2, got %d", suggestions.DescCol)
	}

	if suggestions.DebitCol != 3 {
		t.Errorf("Expected debit column 3, got %d", suggestions.DebitCol)
	}

	if suggestions.CreditCol != 4 {
		t.Errorf("Expected credit column 4, got %d", suggestions.CreditCol)
	}

	if suggestions.CategoryCol != 6 {
		t.Errorf("Expected category column 6, got %d", suggestions.CategoryCol)
	}

	if !suggestions.IsDoubleEntry {
		t.Error("Expected IsDoubleEntry to be true")
	}
}

func TestSuggestColumns_American(t *testing.T) {
	headers := []string{"Date", "Description", "Amount", "Category"}

	suggestions := SuggestColumns(headers)

	if suggestions.DateCol != 0 {
		t.Errorf("Expected date column 0, got %d", suggestions.DateCol)
	}

	if suggestions.DescCol != 1 {
		t.Errorf("Expected description column 1, got %d", suggestions.DescCol)
	}

	if suggestions.AmountCol != 2 {
		t.Errorf("Expected amount column 2, got %d", suggestions.AmountCol)
	}

	if suggestions.IsDoubleEntry {
		t.Error("Expected IsDoubleEntry to be false for single amount column")
	}
}

func TestGenerateFingerprint_Consistency(t *testing.T) {
	headers1 := []string{"Data mov.", "Descrição", "Débito", "Crédito"}
	headers2 := []string{"Data mov.", "Descrição", "Débito", "Crédito"}
	headers3 := []string{"Date", "Description", "Debit", "Credit"} // Different bank

	fp1 := generateFingerprint(headers1)
	fp2 := generateFingerprint(headers2)
	fp3 := generateFingerprint(headers3)

	// Same headers should produce same fingerprint
	if fp1 != fp2 {
		t.Error("Same headers should produce same fingerprint")
	}

	// Different headers should produce different fingerprint
	if fp1 == fp3 {
		t.Error("Different headers should produce different fingerprint")
	}
}

func TestGenerateFingerprint_CaseInsensitive(t *testing.T) {
	headers1 := []string{"Data mov.", "DESCRIÇÃO", "Débito"}
	headers2 := []string{"data mov.", "descrição", "débito"}

	fp1 := generateFingerprint(headers1)
	fp2 := generateFingerprint(headers2)

	// Should be case-insensitive (normalized to lowercase)
	if fp1 != fp2 {
		t.Error("Fingerprint should be case-insensitive")
	}
}

func TestDetectConfig_NoHeaders(t *testing.T) {
	data := `Just some random text
Without any recognizable headers
Or proper CSV structure`

	_, err := DetectConfig([]byte(data))
	if err != ErrNoHeadersFound {
		t.Errorf("Expected ErrNoHeadersFound, got %v", err)
	}
}

func TestGetSampleRows(t *testing.T) {
	// After header at line 6, should get 3 data rows
	rows := getSampleRows([]byte(samplePortugueseCSV), ';', 7, 5)

	if len(rows) != 3 {
		t.Errorf("Expected 3 sample rows, got %d", len(rows))
	}

	// First row should be Pingo Doce transaction
	if len(rows) > 0 && !strings.Contains(rows[0][2], "Pingo Doce") {
		t.Errorf("First sample row description should contain 'Pingo Doce', got %s", rows[0][2])
	}
}
