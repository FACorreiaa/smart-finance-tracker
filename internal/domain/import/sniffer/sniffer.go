// Package sniffer provides automatic detection of CSV/TSV file formats.
// It identifies delimiters, header rows, and generates fingerprints for bank recognition.
package sniffer

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"unicode"
)

// Common bank statement header keywords (multi-language)
var headerKeywords = []string{
	// Portuguese
	"data mov", "data mov.", "descrição", "descricao", "débito", "debito", "crédito", "credito",
	"data valor", "saldo", "categoria",
	// English
	"date", "description", "amount", "debit", "credit", "balance", "category", "merchant",
	// Spanish
	"fecha", "descripción", "descripcion", "importe", "cargo", "abono",
}

// FileConfig holds the detected configuration for a CSV/TSV file
type FileConfig struct {
	Delimiter   rune       // The field delimiter (';', ',', '\t')
	SkipLines   int        // Number of metadata lines before headers
	Headers     []string   // Detected header names
	Fingerprint string     // SHA256 hash of normalized headers
	SampleRows  [][]string // First few data rows for preview
}

// ColumnSuggestions provides auto-detected column indices
type ColumnSuggestions struct {
	DateCol       int  // Suggested date column index (-1 if not found)
	DescCol       int  // Suggested description column index
	AmountCol     int  // Suggested single amount column (-1 if separate debit/credit)
	DebitCol      int  // Suggested debit column index
	CreditCol     int  // Suggested credit column index
	CategoryCol   int  // Suggested category column index (-1 if not found)
	IsDoubleEntry bool // True if separate debit/credit columns detected
}

var (
	ErrEmptyFile        = errors.New("file is empty")
	ErrNoHeadersFound   = errors.New("could not find data headers")
	ErrInvalidDelimiter = errors.New("could not detect valid delimiter")
)

// DetectConfig analyzes a CSV/TSV file and returns its configuration
func DetectConfig(data []byte) (*FileConfig, error) {
	if len(data) == 0 {
		return nil, ErrEmptyFile
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return nil, ErrEmptyFile
	}

	// Try to find the header row
	delimiter, skipLines, err := findHeaderRow(lines)
	if err != nil {
		return nil, err
	}

	// Parse headers
	headerLine := lines[skipLines]
	reader := csv.NewReader(strings.NewReader(headerLine))
	reader.Comma = delimiter
	reader.LazyQuotes = true

	headers, err := reader.Read()
	if err != nil {
		return nil, err
	}

	// Clean headers
	for i, h := range headers {
		headers[i] = strings.TrimSpace(h)
	}

	// Generate fingerprint
	fingerprint := generateFingerprint(headers)

	// Get sample rows (up to 5)
	sampleRows := getSampleRows(data, delimiter, skipLines+1, 5)

	return &FileConfig{
		Delimiter:   delimiter,
		SkipLines:   skipLines,
		Headers:     headers,
		Fingerprint: fingerprint,
		SampleRows:  sampleRows,
	}, nil
}

// SuggestColumns attempts to auto-match columns based on header names
func SuggestColumns(headers []string) *ColumnSuggestions {
	suggestions := &ColumnSuggestions{
		DateCol:     -1,
		DescCol:     -1,
		AmountCol:   -1,
		DebitCol:    -1,
		CreditCol:   -1,
		CategoryCol: -1,
	}

	for i, header := range headers {
		h := strings.ToLower(strings.TrimSpace(header))

		// Date detection
		if suggestions.DateCol == -1 {
			if strings.Contains(h, "data mov") || strings.Contains(h, "date") ||
				strings.Contains(h, "fecha") || h == "data" {
				suggestions.DateCol = i
			}
		}

		// Description detection
		if suggestions.DescCol == -1 {
			if strings.Contains(h, "descri") || strings.Contains(h, "merchant") ||
				strings.Contains(h, "description") || h == "nome" || h == "name" {
				suggestions.DescCol = i
			}
		}

		// Debit detection
		if suggestions.DebitCol == -1 {
			if strings.Contains(h, "débito") || strings.Contains(h, "debito") ||
				strings.Contains(h, "debit") || strings.Contains(h, "cargo") {
				suggestions.DebitCol = i
			}
		}

		// Credit detection
		if suggestions.CreditCol == -1 {
			if strings.Contains(h, "crédito") || strings.Contains(h, "credito") ||
				strings.Contains(h, "credit") || strings.Contains(h, "abono") {
				suggestions.CreditCol = i
			}
		}

		// Single amount detection
		if suggestions.AmountCol == -1 {
			if h == "amount" || h == "valor" || h == "importe" || h == "montante" {
				suggestions.AmountCol = i
			}
		}

		// Category detection
		if suggestions.CategoryCol == -1 {
			if strings.Contains(h, "categ") || strings.Contains(h, "category") ||
				strings.Contains(h, "tipo") || strings.Contains(h, "type") {
				suggestions.CategoryCol = i
			}
		}
	}

	// Determine if double-entry (separate debit/credit)
	suggestions.IsDoubleEntry = suggestions.DebitCol != -1 && suggestions.CreditCol != -1

	return suggestions
}

// findHeaderRow locates the header row and its delimiter
func findHeaderRow(lines []string) (rune, int, error) {
	delimiters := []rune{';', '\t', ',', '|'}

	for i, line := range lines {
		if i > 20 { // Don't search more than 20 lines
			break
		}

		lineLower := strings.ToLower(line)

		// Check if this line contains header keywords
		hasKeyword := false
		for _, kw := range headerKeywords {
			if strings.Contains(lineLower, kw) {
				hasKeyword = true
				break
			}
		}

		if !hasKeyword {
			continue
		}

		// Found a potential header row, detect delimiter
		for _, d := range delimiters {
			count := strings.Count(line, string(d))
			if count >= 3 { // At least 4 columns
				return d, i, nil
			}
		}
	}

	return 0, 0, ErrNoHeadersFound
}

// generateFingerprint creates a unique hash from header names
func generateFingerprint(headers []string) string {
	// Normalize headers: lowercase, remove non-alphanumeric, sort-ish
	var normalized []string
	for _, h := range headers {
		clean := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToLower(r)
			}
			return -1
		}, h)
		if clean != "" {
			normalized = append(normalized, clean)
		}
	}

	// Join and hash
	joined := strings.Join(normalized, "|")
	hash := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(hash[:])
}

// getSampleRows returns the first N data rows after the header
func getSampleRows(data []byte, delimiter rune, startLine, maxRows int) [][]string {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = delimiter
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // Allow variable fields

	var rows [][]string
	lineNum := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		if lineNum >= startLine {
			rows = append(rows, record)
			if len(rows) >= maxRows {
				break
			}
		}
		lineNum++
	}

	return rows
}
