//go:build integration

package user

import (
	"context"
	"log"
	"log/slog"
	"os"
	"testing"

	echotypes "github.com/FACorreiaa/smart-finance-tracker/internal/domain/common"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	// Your database migration tool/library if you use one programmatically
)

var (
	testUserDB      *pgxpool.Pool
	testUserService UserService // Use the interface
	testUserRepo    UserRepo    // Actual repository implementation for setup/cleanup
)

func TestMain(m *testing.M) {
	// Load .env.test or similar for test database credentials
	if err := godotenv.Load("../../../.env.test"); err != nil { // Adjust path to your .env.test
		log.Println("Warning: .env.test file not found for user integration tests.")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		log.Fatal("TEST_DATABASE_URL environment variable is not set for user integration tests")
	}

	var err error
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Fatalf("Unable to parse TEST_DATABASE_URL: %v\n", err)
	}
	config.MaxConns = 5

	testUserDB, err = pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		log.Fatalf("Unable to create connection pool for user tests: %v\n", err)
	}
	defer testUserDB.Close()

	if err := testUserDB.Ping(context.Background()); err != nil {
		log.Fatalf("Unable to ping test database for user tests: %v\n", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	// Initialize with your *actual* PostgresUserRepo implementation
	// You'll need to export this constructor or make it accessible.
	// For example, if it's in an internal/user/repository package:
	// testUserRepo = repository.NewPostgresUserRepo(testUserDB, logger)
	// For this example, let's assume a constructor NewPostgresUserRepo exists in the current 'user' package for the repo.
	testUserRepo = NewPostgresUserRepo(testUserDB, logger) // Replace with your actual repo constructor
	testUserService = NewUserService(testUserRepo, logger)

	exitCode := m.Run()
	os.Exit(exitCode)
}

// Helper to clean the users table (adjust table name if different)
func clearUsersTable(t *testing.T) {
	t.Helper()
	// Be very careful with DELETE in tests; ensure it's the test DB
	// Consider disabling foreign key checks temporarily if needed for cleanup, or delete in correct order.
	_, err := testUserDB.Exec(context.Background(), "DELETE FROM users")
	require.NoError(t, err, "Failed to clear users table")
}

// Helper to create a user directly for testing setup
func createTestUserDirectly(t *testing.T, user echotypes.User) uuid.UUID {
	t.Helper()
	// This function would use testUserRepo or direct db exec to insert a user for setup
	// It depends on whether your UserRepo has a CreateUser method.
	// Example assuming UserRepo has Create:
	// id, err := testUserRepo.CreateUser(context.Background(), &user) // Or whatever your Create method signature is
	// require.NoError(t, err)
	// return id

	// If not, direct insert:
	var id uuid.UUID
	err := testUserDB.QueryRow(context.Background(),
		"INSERT INTO users (username, email, password_hash, firstname, lastname) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		user.Username, user.Email, user.PasswordHash, user.Firstname, user.Lastname).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestServiceUserImpl_UserProfile_Integration(t *testing.T) {
	ctx := context.Background()
	clearUsersTable(t) // Ensure a clean state

	testUser := echotypes.User{ // Assuming echotypes.User is the struct for your DB table
		Username:     "integ_test_user",
		Email:        "integ@example.com",
		PasswordHash: "hashedpassword", // In real tests, you'd hash a test password
		Firstname:    "Integ",
		Lastname:     "Test",
	}
	createdUserID := createTestUserDirectly(t, testUser)

	t.Run("Get existing user profile", func(t *testing.T) {
		profile, err := testUserService.GetUserProfile(ctx, createdUserID)
		require.NoError(t, err)
		require.NotNil(t, profile)
		assert.Equal(t, createdUserID, profile.ID)
		require.NotNil(t, profile.Username)
		assert.Equal(t, testUser.Username, *profile.Username)
		assert.Equal(t, testUser.Email, profile.Email)
		require.NotNil(t, profile.Firstname)
		require.NotNil(t, profile.Lastname)
		assert.Equal(t, testUser.Firstname, *profile.Firstname)
		assert.Equal(t, testUser.Lastname, *profile.Lastname)
	})

	t.Run("Get non-existent user profile", func(t *testing.T) {
		nonExistentID := uuid.New()
		_, err := testUserService.GetUserProfile(ctx, nonExistentID)
		require.Error(t, err)
		// Check for a specific "not found" error if your repo/service returns one
		assert.Contains(t, err.Error(), "error fetching user profile") // Service wraps it
	})

	t.Run("Update user profile", func(t *testing.T) {
		newUsername := "integ_test_user_updated"
		newFirstname := "IntegrationUpdated"
		newLastname := "TestUpdated"
		phone := "0987654321"
		country := "Testlandia"
		city := "IntegCity"

		updateParams := echotypes.UpdateProfileParams{
			Username:    &newUsername,
			Firstname:   &newFirstname,
			Lastname:    &newLastname,
			PhoneNumber: &phone,
			Country:     &country,
			City:        &city,
		}
		err := testUserService.UpdateUserProfile(ctx, createdUserID, updateParams)
		require.NoError(t, err)

		updatedProfile, err := testUserService.GetUserProfile(ctx, createdUserID)
		require.NoError(t, err)
		require.NotNil(t, updatedProfile)
		require.NotNil(t, updatedProfile.Username)
		require.NotNil(t, updatedProfile.Firstname)
		require.NotNil(t, updatedProfile.Lastname)
		require.NotNil(t, updatedProfile.PhoneNumber)
		require.NotNil(t, updatedProfile.Country)
		require.NotNil(t, updatedProfile.City)
		assert.Equal(t, newUsername, *updatedProfile.Username)
		assert.Equal(t, newFirstname, *updatedProfile.Firstname)
		assert.Equal(t, newLastname, *updatedProfile.Lastname)
		assert.Equal(t, phone, *updatedProfile.PhoneNumber)
		assert.Equal(t, country, *updatedProfile.Country)
		assert.Equal(t, city, *updatedProfile.City)
	})
}

func TestServiceUserImpl_UserStatus_Integration(t *testing.T) {
	ctx := context.Background()
	clearUsersTable(t)

	testUser := echotypes.User{Username: "status_user", Email: "status@example.com", PasswordHash: "hash"}
	userID := createTestUserDirectly(t, testUser)

	t.Run("Update Last Login", func(t *testing.T) {
		// Get initial last_login (might be NULL or default)
		// For this test, we just ensure the call doesn't error out.
		// Verifying the timestamp change precisely can be tricky.
		err := testUserService.UpdateLastLogin(ctx, userID)
		require.NoError(t, err)

		// Optionally, fetch user and check if last_login is recent
		// profile, _ := testUserService.GetUserProfile(ctx, userID)
		// assert.WithinDuration(t, time.Now(), *profile.LastLogin, 5*time.Second)
	})

	t.Run("Mark Email As Verified", func(t *testing.T) {
		err := testUserService.MarkEmailAsVerified(ctx, userID)
		require.NoError(t, err)

		profile, err := testUserService.GetUserProfile(ctx, userID)
		require.NoError(t, err)
		assert.NotNil(t, profile.EmailVerifiedAt)
	})

	t.Run("Deactivate and Reactivate User", func(t *testing.T) {
		// Deactivate
		err := testUserService.DeactivateUser(ctx, userID)
		require.NoError(t, err)
		_, err = testUserService.GetUserProfile(ctx, userID)
		require.Error(t, err)

		// Reactivate
		err = testUserService.ReactivateUser(ctx, userID)
		require.NoError(t, err)
		profile, err := testUserService.GetUserProfile(ctx, userID)
		require.NoError(t, err)
		assert.True(t, profile.IsActive)
	})
}

// To run integration tests:
// TEST_DATABASE_URL="postgres://user:password@localhost:5432/test_db_name?sslmode=disable" go test -v ./internal/user -tags=integration -count=1
