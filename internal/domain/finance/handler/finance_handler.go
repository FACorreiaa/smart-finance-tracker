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
	"google.golang.org/protobuf/types/known/timestamppb"

	"buf.build/gen/go/echo-tracker/echo/connectrpc/go/echo/v1/echov1connect"
	echov1 "buf.build/gen/go/echo-tracker/echo/protocolbuffers/go/echo/v1"
	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/import/repository"
	importservice "github.com/FACorreiaa/smart-finance-tracker/internal/domain/import/service"
	"github.com/FACorreiaa/smart-finance-tracker/pkg/interceptors"
)

// FinanceHandler implements the FinanceService Connect handlers.
type FinanceHandler struct {
	echov1connect.UnimplementedFinanceServiceHandler
	importSvc  *importservice.ImportService
	importRepo repository.ImportRepository
}

// NewFinanceHandler constructs a new handler.
func NewFinanceHandler(importSvc *importservice.ImportService, repo repository.ImportRepository) *FinanceHandler {
	return &FinanceHandler{
		importSvc:  importSvc,
		importRepo: repo,
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
		HeaderRows:      int(req.Msg.HeaderRows),
		Timezone:        req.Msg.Timezone,
		InstitutionName: req.Msg.InstitutionName,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if result.RowsImported == 0 && len(result.Errors) > 0 {
		errMsg := formatImportErrors(result.Errors)
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New(errMsg))
	}

	// Calculate duplicates: parsed rows minus rows that were actually inserted
	// RowsTotal = successfully parsed rows, RowsImported = actually inserted (deduplicated)
	duplicates := result.RowsTotal - result.RowsImported - result.RowsFailed
	if duplicates < 0 {
		duplicates = 0
	}

	return connect.NewResponse(&echov1.ImportTransactionsCsvResponse{
		ImportedCount:  int32(result.RowsImported),
		DuplicateCount: int32(duplicates),
		ImportJobId:    result.JobID.String(),
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

// ListTransactions returns a paginated list of transactions with optional filters.
func (h *FinanceHandler) ListTransactions(
	ctx context.Context,
	req *connect.Request[echov1.ListTransactionsRequest],
) (*connect.Response[echov1.ListTransactionsResponse], error) {
	// Get user ID from auth context
	userIDStr, ok := interceptors.GetUserIDFromContext(ctx)
	if !ok || userIDStr == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("invalid user ID in context"))
	}

	// Build filter from request
	filter := repository.ListTransactionsFilter{
		Limit:  50, // Default
		Offset: 0,
	}

	// Parse pagination
	if req.Msg.Page != nil {
		filter.Limit = int(req.Msg.Page.PageSize)
		// Token-based pagination: decode offset from page token if provided
		if req.Msg.Page.PageToken != "" {
			// Parse page token as offset
			offset, err := strconv.Atoi(req.Msg.Page.PageToken)
			if err == nil && offset > 0 {
				filter.Offset = offset
			}
		}
	}

	// Parse optional filters
	if req.Msg.AccountId != nil && *req.Msg.AccountId != "" {
		parsed, err := uuid.Parse(*req.Msg.AccountId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid account_id"))
		}
		filter.AccountID = &parsed
	}

	if req.Msg.CategoryId != nil && *req.Msg.CategoryId != "" {
		parsed, err := uuid.Parse(*req.Msg.CategoryId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid category_id"))
		}
		filter.CategoryID = &parsed
	}

	if req.Msg.ImportJobId != nil && *req.Msg.ImportJobId != "" {
		parsed, err := uuid.Parse(*req.Msg.ImportJobId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid import_job_id"))
		}
		filter.ImportJobID = &parsed
	}

	if req.Msg.TimeRange != nil {
		if req.Msg.TimeRange.StartTime != nil {
			t := req.Msg.TimeRange.StartTime.AsTime()
			filter.StartDate = &t
		}
		if req.Msg.TimeRange.EndTime != nil {
			t := req.Msg.TimeRange.EndTime.AsTime()
			filter.EndDate = &t
		}
	}

	// Query transactions
	transactions, totalCount, err := h.importRepo.ListTransactions(ctx, userID, filter)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list transactions: %w", err))
	}

	// Convert to proto
	protoTxs := make([]*echov1.Transaction, 0, len(transactions))
	for _, tx := range transactions {
		protoTx := transactionToProto(tx)
		protoTxs = append(protoTxs, protoTx)
	}

	// Build next page token
	var nextPageToken string
	if int64(filter.Offset+filter.Limit) < totalCount {
		nextPageToken = strconv.Itoa(filter.Offset + filter.Limit)
	}

	return connect.NewResponse(&echov1.ListTransactionsResponse{
		Transactions: protoTxs,
		Page: &echov1.PageResponse{
			NextPageToken: nextPageToken,
		},
	}), nil
}

// transactionToProto converts a repository Transaction to proto Transaction
func transactionToProto(tx *repository.Transaction) *echov1.Transaction {
	result := &echov1.Transaction{
		Id:          tx.ID.String(),
		UserId:      tx.UserID.String(),
		Description: tx.Description,
		PostedAt:    timestamppb.New(tx.Date),
		CreatedAt:   timestamppb.New(tx.CreatedAt),
		UpdatedAt:   timestamppb.New(tx.UpdatedAt),
		Source:      echov1.TransactionSource(echov1.TransactionSource_value["TRANSACTION_SOURCE_"+strings.ToUpper(tx.Source)]),
	}

	if tx.AccountID != nil {
		s := tx.AccountID.String()
		result.AccountId = &s
	}
	if tx.CategoryID != nil {
		s := tx.CategoryID.String()
		result.CategoryId = &s
	}
	if tx.ExternalID != nil {
		result.ExternalId = *tx.ExternalID
	}
	if tx.Notes != nil {
		result.Notes = *tx.Notes
	}
	if tx.MerchantName != nil {
		result.MerchantName = *tx.MerchantName
	}
	if tx.OriginalDescription != nil {
		result.OriginalDescription = *tx.OriginalDescription
	}
	if tx.InstitutionName != nil {
		result.InstitutionName = *tx.InstitutionName
	}

	// Convert amount
	result.Amount = &echov1.Money{
		AmountMinor:  tx.AmountCents,
		CurrencyCode: tx.CurrencyCode,
	}

	return result
}

// DeleteImportBatch deletes all transactions from a specific import batch.
func (h *FinanceHandler) DeleteImportBatch(
	ctx context.Context,
	req *connect.Request[echov1.DeleteImportBatchRequest],
) (*connect.Response[echov1.DeleteImportBatchResponse], error) {
	// Get user ID from auth context
	userIDStr, ok := interceptors.GetUserIDFromContext(ctx)
	if !ok || userIDStr == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("invalid user ID in context"))
	}

	importJobID, err := uuid.Parse(req.Msg.ImportJobId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid import_job_id"))
	}

	deletedCount, err := h.importRepo.DeleteByImportJobID(ctx, userID, importJobID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete import batch: %w", err))
	}

	return connect.NewResponse(&echov1.DeleteImportBatchResponse{
		DeletedCount: int32(deletedCount),
	}), nil
}
