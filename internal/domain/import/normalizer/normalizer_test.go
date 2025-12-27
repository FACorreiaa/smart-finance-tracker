package normalizer

import (
	"testing"
	"time"
)

func TestParseAmount_European(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"45,23", 4523},
		{"1.234,56", 123456},
		{"1.000.000,00", 100000000},
		{"0,99", 99},
		{"12,99", 1299},
		{"-45,23", -4523},
		{"", 0},
		{"  45,23  ", 4523},
		{"€ 45,23", 4523}, // Currency symbol stripped
	}

	for _, tc := range tests {
		got, err := ParseAmount(tc.input, true)
		if err != nil {
			t.Errorf("ParseAmount(%q, true) error: %v", tc.input, err)
			continue
		}
		if got != tc.expected {
			t.Errorf("ParseAmount(%q, true) = %d, want %d", tc.input, got, tc.expected)
		}
	}
}

func TestParseAmount_American(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"45.23", 4523},
		{"1,234.56", 123456},
		{"1,000,000.00", 100000000},
		{"0.99", 99},
		{"-29.99", -2999},
		{"", 0},
		{"$45.23", 4523}, // Currency symbol stripped
	}

	for _, tc := range tests {
		got, err := ParseAmount(tc.input, false)
		if err != nil {
			t.Errorf("ParseAmount(%q, false) error: %v", tc.input, err)
			continue
		}
		if got != tc.expected {
			t.Errorf("ParseAmount(%q, false) = %d, want %d", tc.input, got, tc.expected)
		}
	}
}

func TestNormalizeDebitCredit(t *testing.T) {
	tests := []struct {
		debit    string
		credit   string
		european bool
		expected int64
	}{
		// Portuguese bank: debit is expense (negative)
		{"45,23", "", true, -4523},
		{"", "500,00", true, 50000},
		{"12,99", "", true, -1299},

		// Empty both
		{"", "", true, 0},

		// American format
		{"29.99", "", false, -2999},
		{"", "2500.00", false, 250000},
	}

	for _, tc := range tests {
		got, err := NormalizeDebitCredit(tc.debit, tc.credit, tc.european)
		if err != nil {
			t.Errorf("NormalizeDebitCredit(%q, %q) error: %v", tc.debit, tc.credit, err)
			continue
		}
		if got != tc.expected {
			t.Errorf("NormalizeDebitCredit(%q, %q) = %d, want %d", tc.debit, tc.credit, got, tc.expected)
		}
	}
}

func TestParseFlexibleDate(t *testing.T) {
	tests := []struct {
		input    string
		format   string
		expected string // YYYY-MM-DD format
	}{
		// European DD-MM-YYYY
		{"02-01-2024", "DD-MM-YYYY", "2024-01-02"},
		{"25-12-2024", "", "2024-12-25"},
		{"02/01/2024", "DD/MM/YYYY", "2024-01-02"},

		// American MM/DD/YYYY
		{"01/02/2024", "MM/DD/YYYY", "2024-01-02"},

		// ISO YYYY-MM-DD
		{"2024-01-02", "", "2024-01-02"},
		{"2024/01/02", "", "2024-01-02"},
	}

	for _, tc := range tests {
		got, err := ParseFlexibleDate(tc.input, tc.format, time.UTC)
		if err != nil {
			t.Errorf("ParseFlexibleDate(%q, %q) error: %v", tc.input, tc.format, err)
			continue
		}
		gotStr := got.Format("2006-01-02")
		if gotStr != tc.expected {
			t.Errorf("ParseFlexibleDate(%q, %q) = %s, want %s", tc.input, tc.format, gotStr, tc.expected)
		}
	}
}

func TestParseFlexibleDate_Invalid(t *testing.T) {
	_, err := ParseFlexibleDate("", "", nil)
	if err != ErrInvalidDate {
		t.Errorf("Expected ErrInvalidDate for empty string, got %v", err)
	}

	_, err = ParseFlexibleDate("not-a-date", "", nil)
	if err != ErrInvalidDate {
		t.Errorf("Expected ErrInvalidDate for invalid string, got %v", err)
	}
}

func TestDetectDateFormat(t *testing.T) {
	tests := []struct {
		samples  []string
		expected string
	}{
		{[]string{"25-12-2024"}, "DD-MM-YYYY"}, // Day > 12, definitely DD-MM
		{[]string{"25/12/2024"}, "DD/MM/YYYY"}, // Day > 12, definitely DD/MM
		{[]string{"2024-12-25"}, "YYYY-MM-DD"}, // ISO format
		{[]string{"2024/12/25"}, "YYYY/MM/DD"}, // ISO with slash
		{[]string{}, "DD-MM-YYYY"},             // Default
	}

	for _, tc := range tests {
		got := DetectDateFormat(tc.samples)
		if got != tc.expected {
			t.Errorf("DetectDateFormat(%v) = %s, want %s", tc.samples, got, tc.expected)
		}
	}
}

func TestConvertDateFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"DD-MM-YYYY", "02-01-2006"},
		{"MM/DD/YYYY", "01/02/2006"},
		{"YYYY-MM-DD", "2006-01-02"},
		{"DD/MM/YY", "02/01/06"},
	}

	for _, tc := range tests {
		got := convertDateFormat(tc.input)
		if got != tc.expected {
			t.Errorf("convertDateFormat(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestCleanDescription(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  Pingo Doce  ", "Pingo Doce"},
		{"Compra  MB   -   Lidl", "Compra MB - Lidl"},
		{"Netflix", "Netflix"},
	}

	for _, tc := range tests {
		got := CleanDescription(tc.input)
		if got != tc.expected {
			t.Errorf("CleanDescription(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
