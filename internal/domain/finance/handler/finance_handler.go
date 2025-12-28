// Package handler implements the FinanceService Connect RPC handlers.
package handler

import (
	"context"
	"errors"
	"strconv"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"buf.build/gen/go/echo-tracker/echo/connectrpc/go/echo/v1/echov1connect"
	echov1 "buf.build/gen/go/echo-tracker/echo/protocolbuffers/go/echo/v1"
	importservice "github.com/FACorreiaa/smart-finance-tracker/internal/domain/import/service"
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
	userIDStr, ok := getContextValue(ctx, "user_id")
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
	result, err := h.importSvc.ImportWithMapping(ctx, userID, accountID, req.Msg.CsvBytes, mapping)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
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
			DateCol:          0,
			DescCol:          1,
			AmountCol:        2,
			CategoryCol:      -1,
			DebitCol:         -1,
			CreditCol:        -1,
			IsDoubleEntry:    false,
			IsEuropeanFormat: true, // Default to European format
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

	return importservice.ColumnMapping{
		DateCol:          dateCol,
		DescCol:          descCol,
		AmountCol:        amountCol,
		CategoryCol:      -1, // Could add to proto if needed
		DebitCol:         debitCol,
		CreditCol:        creditCol,
		IsDoubleEntry:    isDoubleEntry,
		IsEuropeanFormat: true, // Could add to proto if needed
		DateFormat:       dateFormat,
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

// getContextValue extracts a string value from context.
func getContextValue(ctx context.Context, key string) (string, bool) {
	val := ctx.Value(key)
	if val == nil {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}
