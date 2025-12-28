// Package handler implements the FinanceService Connect RPC handlers.
package handler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"buf.build/gen/go/echo-tracker/echo/connectrpc/go/echo/v1/echov1connect"
	echov1 "buf.build/gen/go/echo-tracker/echo/protocolbuffers/go/echo/v1"
	importservice "github.com/FACorreiaa/smart-finance-tracker/internal/domain/import/service"
	"github.com/FACorreiaa/smart-finance-tracker/pkg/interceptors"
)

// FinanceHandler implements the FinanceService Connect handlers.
type FinanceHandler struct {
	echov1connect.UnimplementedFinanceServiceHandler
	importSvc *importservice.ImportService
}

// NewFinanceHandler constructs a new handler.
func NewFinanceHandler(importSvc *importservice.ImportService) *FinanceHandler {
	return &FinanceHandler{
		importSvc: importSvc,
	}
}

// ImportTransactionsCsv handles CSV transaction import with column mapping.
func (h *FinanceHandler) ImportTransactionsCsv(
	ctx context.Context,
	req *connect.Request[echov1.ImportTransactionsCsvRequest],
) (*connect.Response[echov1.ImportTransactionsCsvResponse], error) {
	// Get user ID from auth context
	userIDStr, ok := interceptors.GetUserIDFromContext(ctx)
	if !ok || userIDStr == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("invalid user ID in context"))
	}

	// Parse optional account ID
	var accountID *uuid.UUID
	if req.Msg.AccountId != nil && *req.Msg.AccountId != "" {
		parsed, err := uuid.Parse(*req.Msg.AccountId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid account_id"))
		}
		accountID = &parsed
	}

	// Validate CSV bytes
	if len(req.Msg.CsvBytes) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("csv_bytes is required"))
	}

	// Convert proto CsvMapping to service ColumnMapping
	mapping := h.protoMappingToService(req.Msg.Mapping, req.Msg.DateFormat)

	// Perform import
	result, err := h.importSvc.ImportWithOptions(ctx, userID, accountID, req.Msg.CsvBytes, mapping, importservice.ImportOptions{
		HeaderRows: int(req.Msg.HeaderRows),
		Timezone:   req.Msg.Timezone,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if result.RowsImported == 0 && len(result.Errors) > 0 {
		errMsg := formatImportErrors(result.Errors)
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New(errMsg))
	}

	return connect.NewResponse(&echov1.ImportTransactionsCsvResponse{
		ImportedCount: int32(result.RowsImported),
	}), nil
}

// protoMappingToService converts a proto CsvMapping to the service's ColumnMapping.
func (h *FinanceHandler) protoMappingToService(protoMapping *echov1.CsvMapping, dateFormat string) importservice.ColumnMapping {
	// Default mapping if none provided
	if protoMapping == nil {
		return importservice.ColumnMapping{
			DateCol:          -1,
			DescCol:          -1,
			AmountCol:        -1,
			CategoryCol:      -1,
			DebitCol:         -1,
			CreditCol:        -1,
			IsDoubleEntry:    false,
			IsEuropeanFormat: true, // Default to European for Portuguese/EU banks
			DateFormat:       dateFormat,
		}
	}

	// Parse column indices from column names (headers) or indices
	// The proto uses string for flexibility (could be header name or index)
	dateCol := parseColumnIndex(protoMapping.DateColumn)
	descCol := parseColumnIndex(protoMapping.DescriptionColumn)
	amountCol := parseColumnIndex(protoMapping.AmountColumn)
	debitCol := parseColumnIndex(protoMapping.DebitColumn)
	creditCol := parseColumnIndex(protoMapping.CreditColumn)

	// Determine if double entry based on whether debit/credit columns are set
	isDoubleEntry := debitCol >= 0 && creditCol >= 0

	// Parse delimiter from proto (single character string to rune)
	var delimiter rune
	if protoMapping.Delimiter != "" {
		delimiter = rune(protoMapping.Delimiter[0])
	}

	return importservice.ColumnMapping{
		DateCol:          dateCol,
		DescCol:          descCol,
		AmountCol:        amountCol,
		CategoryCol:      -1, // Could add to proto if needed
		DebitCol:         debitCol,
		CreditCol:        creditCol,
		IsDoubleEntry:    isDoubleEntry,
		IsEuropeanFormat: getIsEuropeanFormat(protoMapping),
		DateFormat:       dateFormat,
		Delimiter:        delimiter,
		SkipLines:        int(protoMapping.SkipLines),
	}
}

// parseColumnIndex converts a string column identifier to an int index.
// If it's a number string, parses as int. Otherwise returns -1.
func parseColumnIndex(col string) int {
	if col == "" {
		return -1
	}
	idx, err := strconv.Atoi(col)
	if err != nil {
		return -1
	}
	return idx
}

// getIsEuropeanFormat extracts the is_european_format field from the proto.
// Returns true (European format) as default for backwards compatibility.
func getIsEuropeanFormat(protoMapping *echov1.CsvMapping) bool {
	if protoMapping == nil {
		return true
	}
	return protoMapping.GetIsEuropeanFormat()
}

const maxImportErrorsInResponse = 10

func formatImportErrors(errors []string) string {
	if len(errors) == 0 {
		return "import failed: no valid rows"
	}

	limit := len(errors)
	if limit > maxImportErrorsInResponse {
		limit = maxImportErrorsInResponse
	}

	message := fmt.Sprintf("import failed: %d error(s). ", len(errors))
	message += strings.Join(errors[:limit], "; ")
	if limit < len(errors) {
		message += fmt.Sprintf(" (and %d more)", len(errors)-limit)
	}

	return message
}
