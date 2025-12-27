// Package normalizer handles regional money and date parsing.
// Converts various bank statement formats into Echo's canonical representation.
package normalizer

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	ErrInvalidAmount = errors.New("invalid amount format")
	ErrInvalidDate   = errors.New("invalid date format")
)

// AmountConfig specifies how to parse amount strings
type AmountConfig struct {
	IsEuropean    bool // European format: 1.234,56 vs American: 1,234.56
	IsDoubleEntry bool // Separate debit/credit columns vs single amount
}

// ParseAmount converts a string amount to cents (int64)
// Supports both European (1.234,56) and American (1,234.56) formats
func ParseAmount(raw string, isEuropean bool) (int64, error) {
	if raw == "" {
		return 0, nil
	}

	// Clean the string: keep digits, comma, period, and minus
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) || r == ',' || r == '.' || r == '-' {
			return r
		}
		return -1
	}, raw)

	if cleaned == "" {
		return 0, nil
	}

	// Handle negative sign
	isNegative := strings.HasPrefix(cleaned, "-")
	cleaned = strings.TrimPrefix(cleaned, "-")

	if isEuropean {
		// European: 1.234,56 -> 1234.56
		// Remove thousands separator (period), convert decimal separator (comma) to period
		cleaned = strings.ReplaceAll(cleaned, ".", "")
		cleaned = strings.ReplaceAll(cleaned, ",", ".")
	} else {
		// American: 1,234.56 -> 1234.56
		// Just remove thousands separator (comma)
		cleaned = strings.ReplaceAll(cleaned, ",", "")
	}

	val, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, ErrInvalidAmount
	}

	// Convert to cents (int64)
	cents := int64(math.Round(val * 100))

	if isNegative {
		cents = -cents
	}

	return cents, nil
}

// NormalizeDebitCredit merges separate debit and credit columns into a single signed amount
// Debit = negative (money out), Credit = positive (money in)
func NormalizeDebitCredit(debitStr, creditStr string, isEuropean bool) (int64, error) {
	// Clean strings
	debitStr = strings.TrimSpace(debitStr)
	creditStr = strings.TrimSpace(creditStr)

	// Parse debit (negative)
	if debitStr != "" {
		amount, err := ParseAmount(debitStr, isEuropean)
		if err != nil {
			return 0, err
		}
		// Ensure it's negative
		if amount > 0 {
			amount = -amount
		}
		return amount, nil
	}

	// Parse credit (positive)
	if creditStr != "" {
		amount, err := ParseAmount(creditStr, isEuropean)
		if err != nil {
			return 0, err
		}
		// Ensure it's positive
		if amount < 0 {
			amount = -amount
		}
		return amount, nil
	}

	return 0, nil
}

// Common date formats used by banks worldwide
var dateFormats = []string{
	// European (DD-MM-YYYY variants)
	"02-01-2006",
	"02/01/2006",
	"02.01.2006",
	"2-1-2006",
	"2/1/2006",

	// American (MM-DD-YYYY variants)
	"01-02-2006",
	"01/02/2006",
	"1/2/2006",

	// ISO (YYYY-MM-DD)
	"2006-01-02",
	"2006/01/02",

	// With time
	"02-01-2006 15:04",
	"02/01/2006 15:04",
	"01/02/2006 15:04",
	"2006-01-02 15:04:05",
}

// ParseFlexibleDate attempts to parse a date using multiple formats
func ParseFlexibleDate(raw string, preferredFormat string, loc *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, ErrInvalidDate
	}

	if loc == nil {
		loc = time.UTC
	}

	// Try preferred format first
	if preferredFormat != "" {
		goFormat := convertDateFormat(preferredFormat)
		if t, err := time.ParseInLocation(goFormat, raw, loc); err == nil {
			return t, nil
		}
	}

	// Try all known formats
	for _, format := range dateFormats {
		if t, err := time.ParseInLocation(format, raw, loc); err == nil {
			return t, nil
		}
	}

	return time.Time{}, ErrInvalidDate
}

// convertDateFormat converts user-friendly format strings to Go format
// e.g., "DD-MM-YYYY" -> "02-01-2006"
func convertDateFormat(format string) string {
	replacements := map[string]string{
		"YYYY": "2006",
		"YY":   "06",
		"MM":   "01",
		"DD":   "02",
		"HH":   "15",
		"mm":   "04",
		"ss":   "05",
	}

	result := format
	for pattern, goFmt := range replacements {
		result = strings.ReplaceAll(result, pattern, goFmt)
	}
	return result
}

// DetectDateFormat attempts to guess the date format from sample data
func DetectDateFormat(samples []string) string {
	if len(samples) == 0 {
		return "DD-MM-YYYY"
	}

	// Check first sample
	sample := strings.TrimSpace(samples[0])

	// Pattern matching
	ddmmyyyyPattern := regexp.MustCompile(`^\d{1,2}[-/]\d{1,2}[-/]\d{4}$`)
	isoPattern := regexp.MustCompile(`^\d{4}[-/]\d{1,2}[-/]\d{1,2}$`)

	if isoPattern.MatchString(sample) {
		if strings.Contains(sample, "/") {
			return "YYYY/MM/DD"
		}
		return "YYYY-MM-DD"
	}

	if ddmmyyyyPattern.MatchString(sample) {
		// Check if day is > 12 (definitely DD-MM-YYYY)
		parts := strings.FieldsFunc(sample, func(r rune) bool {
			return r == '-' || r == '/'
		})
		if len(parts) >= 2 {
			day, _ := strconv.Atoi(parts[0])
			if day > 12 {
				if strings.Contains(sample, "/") {
					return "DD/MM/YYYY"
				}
				return "DD-MM-YYYY"
			}
		}

		// Default to European format (more common globally)
		if strings.Contains(sample, "/") {
			return "DD/MM/YYYY"
		}
		return "DD-MM-YYYY"
	}

	// Default
	return "DD-MM-YYYY"
}

// CleanDescription normalizes merchant/description text
func CleanDescription(raw string) string {
	// Trim whitespace
	result := strings.TrimSpace(raw)

	// Collapse multiple spaces
	spacePattern := regexp.MustCompile(`\s+`)
	result = spacePattern.ReplaceAllString(result, " ")

	return result
}
