package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	echov1 "github.com/FACorreiaa/smart-finance-tracker-proto/gen/go/echo/v1"

	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/auth/repository"
	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/auth/service"
	"github.com/FACorreiaa/smart-finance-tracker/internal/domain/auth/servicetest"
)

func TestAuthHandler_Register_Success(t *testing.T) {
	ctx := context.Background()
	svc, repo, tokens, emails := servicetest.NewTestAuthService()
	handler := NewAuthHandler(svc)

	tokens.GenerateFunc = func(_, _, _, _ string) (*service.TokenPair, error) {
		return &service.TokenPair{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			ExpiresAt:    time.Now().Add(time.Hour),
			TokenType:    "Bearer",
		}, nil
	}

	username := "rpcuser"
	req := connect.NewRequest(&echov1.RegisterRequest{
		Email:    "rpc-register@example.com",
		Username: &username,
		Password: "Str0ng!Pass",
	})
	req.Header().Set("User-Agent", "rpc-test-agent")

	resp, err := handler.Register(ctx, req)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.Msg == nil || resp.Msg.User == nil {
		t.Fatalf("expected user in response, got %#v", resp.Msg)
	}

	if _, err := repo.GetUserByEmail(ctx, "rpc-register@example.com"); err != nil {
		t.Fatalf("user not stored: %v", err)
	}
	servicetest.WaitFor(t, func() bool { return emails.VerificationSent() })
	if len(repo.Sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(repo.Sessions))
	}
	for _, session := range repo.Sessions {
		if session.UserAgent == nil || *session.UserAgent != "rpc-test-agent" {
			t.Fatalf("expected user agent stored, got %v", session.UserAgent)
		}
		if session.ClientIP == nil || *session.ClientIP != "unknown" {
			t.Fatalf("expected default client IP stored, got %v", session.ClientIP)
		}
	}
}

func TestAuthHandler_Register_InvalidInput(t *testing.T) {
	handler := NewAuthHandler(nil)

	_, err := handler.Register(context.Background(), connect.NewRequest(&echov1.RegisterRequest{}))
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected invalid argument, got %v", connect.CodeOf(err))
	}
}

func TestAuthHandler_Login_Success(t *testing.T) {
	ctx := context.Background()
	svc, repo, tokens, _ := servicetest.NewTestAuthService()
	handler := NewAuthHandler(svc)

	hashed := servicetest.MustHash(t, "Str0ng!Pass")
	user := servicetest.AddUser(repo, t, "rpc-login@example.com", true, hashed)
	tokens.GenerateFunc = func(_, _, _, _ string) (*service.TokenPair, error) {
		return &service.TokenPair{
			AccessToken:  "login-access",
			RefreshToken: "login-refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
		}, nil
	}

	req := connect.NewRequest(&echov1.LoginRequest{
		Email:    user.Email,
		Password: "Str0ng!Pass",
	})
	req.Header().Set("User-Agent", "login-agent")

	resp, err := handler.Login(ctx, req)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if resp.Msg.Tokens == nil || resp.Msg.Tokens.AccessToken != "login-access" || resp.Msg.Tokens.RefreshToken != "login-refresh" {
		t.Fatalf("unexpected tokens: %#v", resp.Msg)
	}
	if len(repo.Sessions) != 1 {
		t.Fatalf("session not stored")
	}
}

func TestAuthHandler_Login_InvalidCredentials(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := servicetest.NewTestAuthService()
	handler := NewAuthHandler(svc)

	hashed := servicetest.MustHash(t, "Str0ng!Pass")
	servicetest.AddUser(repo, t, "badlogin@example.com", true, hashed)

	_, err := handler.Login(ctx, connect.NewRequest(&echov1.LoginRequest{
		Email:    "badlogin@example.com",
		Password: "WrongPass!1",
	}))
	if err == nil {
		t.Fatalf("expected error")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("expected unauthenticated, got %v", connect.CodeOf(err))
	}
}

func TestAuthHandler_Refresh_Success(t *testing.T) {
	ctx := context.Background()
	svc, repo, tokens, _ := servicetest.NewTestAuthService()
	handler := NewAuthHandler(svc)

	user, err := repo.CreateUser(ctx, "refresh@example.com", "rpc", "hashed", "RPC User")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	user.IsActive = true
	repo.Users[user.Email] = user

	oldRefresh := "old-refresh"
	if _, err := repo.CreateUserSession(ctx, user.ID, hashTestToken(oldRefresh), "agent", "ip", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateUserSession: %v", err)
	}

	tokens.RefreshFunc = func(token string) (*service.Claims, error) {
		if token != oldRefresh {
			return nil, errors.New("unexpected token")
		}
		return &service.Claims{UserID: user.ID.String()}, nil
	}
	tokens.GenerateFunc = func(_, _, _, _ string) (*service.TokenPair, error) {
		return &service.TokenPair{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
		}, nil
	}

	resp, err := handler.Refresh(ctx, connect.NewRequest(&echov1.RefreshRequest{
		RefreshToken: oldRefresh,
	}))
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if resp.Msg.Tokens == nil || resp.Msg.Tokens.RefreshToken != "new-refresh" {
		t.Fatalf("unexpected refresh token %v", resp.Msg)
	}
	if len(repo.Sessions) != 1 {
		t.Fatalf("expected one session after refresh, got %d", len(repo.Sessions))
	}
	if _, ok := repo.Sessions[hashTestToken(oldRefresh)]; ok {
		t.Fatalf("old session should be removed")
	}
	if _, ok := repo.Sessions[hashTestToken("new-refresh")]; !ok {
		t.Fatalf("new session should be stored")
	}
}

func TestAuthHandler_GetMe(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := servicetest.NewTestAuthService()
	handler := NewAuthHandler(svc)

	// Add user details to context (simulating auth interceptor)
	ctx = context.WithValue(ctx, "user_id", "user-id")
	ctx = context.WithValue(ctx, "email", "rpc@example.com")
	ctx = context.WithValue(ctx, "username", "rpcuser")

	resp, err := handler.GetMe(ctx, connect.NewRequest(&echov1.GetMeRequest{}))
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if resp.Msg.User == nil || resp.Msg.User.Email != "rpc@example.com" {
		t.Fatalf("unexpected response %#v", resp.Msg)
	}
}

func TestAuthHandler_GetMe_Unauthenticated(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := servicetest.NewTestAuthService()
	handler := NewAuthHandler(svc)

	// No user in context
	_, err := handler.GetMe(ctx, connect.NewRequest(&echov1.GetMeRequest{}))
	if err == nil {
		t.Fatalf("expected error for unauthenticated request")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("expected unauthenticated, got %v", connect.CodeOf(err))
	}
}

func TestAuthHandler_Logout(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := servicetest.NewTestAuthService()
	handler := NewAuthHandler(svc)

	user := servicetest.AddUser(repo, t, "logout-rpc@example.com", true, "hashed")
	repo.Sessions[hashTestToken("refresh-token")] = &repository.UserSession{
		ID:        user.ID,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour),
	}

	resp, err := handler.Logout(ctx, connect.NewRequest(&echov1.LogoutRequest{
		RefreshToken: "refresh-token",
	}))
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if resp.Msg == nil {
		t.Fatalf("unexpected response %#v", resp.Msg)
	}
	if len(repo.Sessions) != 0 {
		t.Fatalf("session should be removed")
	}
}

func TestAuthHandler_ErrorMappings(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := servicetest.NewTestAuthService()
	handler := NewAuthHandler(svc)

	// Inactive account triggers permission denied.
	user := servicetest.AddUser(repo, t, "inactive@example.com", false, servicetest.MustHash(t, "Str0ng!Pass"))
	req := connect.NewRequest(&echov1.LoginRequest{
		Email:    user.Email,
		Password: "Str0ng!Pass",
	})
	_, err := handler.Login(ctx, req)
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected permission denied, got %v", connect.CodeOf(err))
	}

	// Unknown email maps to not found.
	_, err = handler.Login(ctx, connect.NewRequest(&echov1.LoginRequest{
		Email:    "missing@example.com",
		Password: "Str0ng!Pass",
	}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("expected not found, got %v", connect.CodeOf(err))
	}
}

func hashTestToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
