package handler

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	echov1 "github.com/FACorreiaa/smart-finance-tracker-proto/gen/go/echo/v1"
	"github.com/FACorreiaa/smart-finance-tracker-proto/gen/go/echo/v1/echov1connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/auth/common"
	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/auth/service"
)

// AuthHandler implements the AuthService Connect handlers.
type AuthHandler struct {
	echov1connect.UnimplementedAuthServiceHandler
	service *service.AuthService
}

// NewAuthHandler constructs a new handler.
func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{
		service: svc,
	}
}

// Register handles user registration RPCs.
func (h *AuthHandler) Register(
	ctx context.Context,
	req *connect.Request[echov1.RegisterRequest],
) (*connect.Response[echov1.AuthResponse], error) {
	if req.Msg.Email == "" || req.Msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("email and password are required"))
	}

	username := ""
	if req.Msg.Username != nil {
		username = *req.Msg.Username
	}

	result, err := h.service.RegisterUser(ctx, service.RegisterParams{
		Email:       req.Msg.Email,
		Username:    username,
		Password:    req.Msg.Password,
		DisplayName: username,
		Metadata:    metadataFromRequest(req),
	})
	if err != nil {
		return nil, h.toConnectError(err)
	}

	return connect.NewResponse(toAuthResponse(result)), nil
}

// Login authenticates a user.
func (h *AuthHandler) Login(
	ctx context.Context,
	req *connect.Request[echov1.LoginRequest],
) (*connect.Response[echov1.AuthResponse], error) {
	if req.Msg.Email == "" || req.Msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("email and password are required"))
	}

	result, err := h.service.Login(ctx, service.LoginParams{
		Email:    req.Msg.Email,
		Password: req.Msg.Password,
		Metadata: metadataFromRequest(req),
	})
	if err != nil {
		return nil, h.toConnectError(err)
	}

	return connect.NewResponse(toAuthResponse(&service.RegisterResult{
		User:   result.User,
		Tokens: result.Tokens,
	})), nil
}

// Refresh issues new access/refresh tokens.
func (h *AuthHandler) Refresh(
	ctx context.Context,
	req *connect.Request[echov1.RefreshRequest],
) (*connect.Response[echov1.AuthResponse], error) {
	if req.Msg.RefreshToken == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("refresh token is required"))
	}

	tokens, err := h.service.RefreshTokens(ctx, service.RefreshTokenParams{
		RefreshToken: req.Msg.RefreshToken,
		Metadata:     metadataFromRequest(req),
	})
	if err != nil {
		return nil, h.toConnectError(err)
	}

	return connect.NewResponse(&echov1.AuthResponse{
		Tokens: &echov1.AuthTokens{
			AccessToken:  tokens.AccessToken,
			RefreshToken: tokens.RefreshToken,
		},
	}), nil
}

// Logout deletes the refresh token session.
func (h *AuthHandler) Logout(
	ctx context.Context,
	req *connect.Request[echov1.LogoutRequest],
) (*connect.Response[echov1.LogoutResponse], error) {
	if req.Msg.RefreshToken == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("refresh token is required"))
	}

	if err := h.service.Logout(ctx, req.Msg.RefreshToken); err != nil {
		return nil, h.toConnectError(err)
	}

	return connect.NewResponse(&echov1.LogoutResponse{}), nil
}

// GetMe returns the current authenticated user's information.
func (h *AuthHandler) GetMe(
	ctx context.Context,
	_ *connect.Request[echov1.GetMeRequest],
) (*connect.Response[echov1.GetMeResponse], error) {
	// User ID comes from auth interceptor context
	userIDStr, ok := getContextValue(ctx, "user_id")
	if !ok || userIDStr == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	email, _ := getContextValue(ctx, "email")
	username, _ := getContextValue(ctx, "username")

	return connect.NewResponse(&echov1.GetMeResponse{
		User: &echov1.User{
			Id:       userIDStr,
			Email:    email,
			Username: username,
		},
	}), nil
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

// toAuthResponse converts service results to proto AuthResponse.
func toAuthResponse(result *service.RegisterResult) *echov1.AuthResponse {
	if result == nil {
		return &echov1.AuthResponse{}
	}

	resp := &echov1.AuthResponse{}

	if result.User != nil {
		resp.User = &echov1.User{
			Id:          result.User.ID.String(),
			Email:       result.User.Email,
			Username:    result.User.Username,
			DisplayName: result.User.DisplayName,
			CreatedAt:   timestamppb.New(result.User.CreatedAt),
			UpdatedAt:   timestamppb.New(result.User.UpdatedAt),
		}
	}

	if result.Tokens != nil {
		resp.Tokens = &echov1.AuthTokens{
			AccessToken:  result.Tokens.AccessToken,
			RefreshToken: result.Tokens.RefreshToken,
		}
	}

	return resp
}

func metadataFromRequest[T any](req *connect.Request[T]) service.SessionMetadata {
	return service.SessionMetadata{
		UserAgent: req.Header().Get("User-Agent"),
		ClientIP:  req.Peer().Addr,
	}
}

func (h *AuthHandler) toConnectError(err error) error {
	switch {
	case errors.Is(err, common.ErrUserAlreadyExists):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, common.ErrInvalidCredentials):
		return connect.NewError(connect.CodeUnauthenticated, err)
	case errors.Is(err, common.ErrInvalidToken), errors.Is(err, common.ErrSessionNotFound):
		return connect.NewError(connect.CodeUnauthenticated, err)
	case errors.Is(err, common.ErrUserNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, service.ErrAccountInactive):
		return connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, service.ErrPasswordTooShort),
		errors.Is(err, service.ErrPasswordNoDigit),
		errors.Is(err, service.ErrPasswordNoLowercase),
		errors.Is(err, service.ErrPasswordNoUppercase),
		errors.Is(err, service.ErrPasswordNoSpecial):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}
