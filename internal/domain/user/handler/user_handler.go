package handler

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/google/uuid"

	"buf.build/gen/go/echo-tracker/echo/connectrpc/go/echo/v1/echov1connect"
	echov1 "buf.build/gen/go/echo-tracker/echo/protocolbuffers/go/echo/v1"
	echotypes "github.com/FACorreiaa/smart-finance-tracker/internal/domain/common"
	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/user"
	"github.com/FACorreiaa/smart-finance-tracker/pkg/interceptors"
)

// UserHandler implements the UserService Connect handlers.
type UserHandler struct {
	echov1connect.UnimplementedUserServiceHandler
	service user.UserService
}

// NewUserHandler constructs a new handler.
func NewUserHandler(svc user.UserService) *UserHandler {
	return &UserHandler{
		service: svc,
	}
}

// GetUser retrieves a user's profile by ID.
func (h *UserHandler) GetUser(
	ctx context.Context,
	req *connect.Request[echov1.GetUserRequest],
) (*connect.Response[echov1.GetUserResponse], error) {
	// Get user ID from request or from context (authenticated user)
	userIDStr := req.Msg.GetUserId()
	if userIDStr == "" {
		var ok bool
		userIDStr, ok = interceptors.GetUserIDFromContext(ctx)
		if !ok || userIDStr == "" {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
		}
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid user id: %w", err))
	}

	profile, err := h.service.GetUserProfile(ctx, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&echov1.GetUserResponse{
		User: toProtoUser(profile),
	}), nil
}

// UpdateUser updates a user's profile.
func (h *UserHandler) UpdateUser(
	ctx context.Context,
	req *connect.Request[echov1.UpdateUserRequest],
) (*connect.Response[echov1.UpdateUserResponse], error) {
	userIDStr, ok := interceptors.GetUserIDFromContext(ctx)
	if !ok || userIDStr == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid user id: %w", err))
	}

	params := echotypes.UpdateProfileParams{}
	if req.Msg.DisplayName != "" {
		params.DisplayName = &req.Msg.DisplayName
	}

	if err := h.service.UpdateUserProfile(ctx, userID, params); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Get updated profile to return
	profile, err := h.service.GetUserProfile(ctx, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&echov1.UpdateUserResponse{
		User: toProtoUser(profile),
	}), nil
}

// toProtoUser converts a domain UserProfile to proto User.
func toProtoUser(p *echotypes.UserProfile) *echov1.User {
	if p == nil {
		return nil
	}

	protoUser := &echov1.User{
		Id:        p.ID.String(),
		Email:     p.Email,
		CreatedAt: timestamppb.New(p.CreatedAt),
		UpdatedAt: timestamppb.New(p.UpdatedAt),
	}

	if p.Username != nil {
		protoUser.Username = *p.Username
	}
	if p.DisplayName != nil {
		protoUser.DisplayName = *p.DisplayName
	}

	return protoUser
}
