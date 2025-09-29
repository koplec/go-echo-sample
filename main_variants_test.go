package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"openapi-validation-example/generated"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestAppVariants creates a test Echo app with database UserHandler
func setupTestAppVariants(t *testing.T, validationMode string) (*echo.Echo, *UserHandler, *DatabaseService) {
	e := echo.New()

	// Setup validation middleware
	var specFile string
	switch validationMode {
	case "flexible":
		specFile = "openapi-flexible.yaml"
	case "strict":
		specFile = "openapi-strict.yaml"
	default:
		specFile = "openapi.yaml"
	}

	validationMiddleware, err := NewValidationMiddleware(specFile)
	require.NoError(t, err)
	e.Use(validationMiddleware.Validate())

	// Create test database
	testDBPath := "test_users_" + validationMode + ".db"
	os.Remove(testDBPath) // Clean up any existing test DB

	db, err := NewDatabaseService(testDBPath)
	require.NoError(t, err)

	userHandler := NewUserHandler(db)

	// Register routes
	generated.RegisterHandlers(e, userHandler)

	// Clean up function
	t.Cleanup(func() {
		db.queries.db.Close()
		os.Remove(testDBPath)
	})

	return e, userHandler, db
}

func TestDatabaseUserHandler_CreateUser(t *testing.T) {
	tests := []struct {
		validationMode string
		name           string
		requestBody    string
		expectedStatus int
		expectError    bool
	}{
		{
			validationMode: "default",
			name:           "Valid user with required fields",
			requestBody:    `{"email": "db-test@example.com", "age": 25}`,
			expectedStatus: http.StatusCreated,
			expectError:    false,
		},
		{
			validationMode: "flexible",
			name:           "Valid user with additional properties",
			requestBody:    `{"email": "flexible@example.com", "age": 30, "hobby": "programming", "location": "Tokyo"}`,
			expectedStatus: http.StatusCreated,
			expectError:    false,
		},
		{
			validationMode: "strict",
			name:           "Invalid - additional property in strict mode",
			requestBody:    `{"email": "strict@example.com", "age": 30, "hobby": "programming"}`,
			expectedStatus: http.StatusInternalServerError,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.validationMode+"_"+tt.name, func(t *testing.T) {
			e, _, _ := setupTestAppVariants(t, tt.validationMode)

			req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(tt.requestBody))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)

			if !tt.expectError && tt.expectedStatus == http.StatusCreated {
				var user generated.User
				err := json.Unmarshal(rec.Body.Bytes(), &user)
				require.NoError(t, err)
				assert.NotZero(t, user.Id)
			}
		})
	}
}

func TestDatabaseUserHandler_GetUser(t *testing.T) {
	e, _, db := setupTestAppVariants(t, "default")

	// Create a test user directly in the database
	userReq := generated.UserRequest{
		Email: "dbget@example.com",
		Age:   28,
	}

	user, err := db.CreateUser(userReq, nil)
	require.NoError(t, err)

	tests := []struct {
		name           string
		userID         string
		expectedStatus int
		expectUser     bool
	}{
		{
			name:           "Get existing user",
			userID:         "1",
			expectedStatus: http.StatusOK,
			expectUser:     true,
		},
		{
			name:           "Get non-existing user",
			userID:         "999",
			expectedStatus: http.StatusNotFound,
			expectUser:     false,
		},
		{
			name:           "Invalid user ID",
			userID:         "invalid",
			expectedStatus: http.StatusBadRequest,
			expectUser:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/users/"+tt.userID, nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)

			if tt.expectUser {
				var retrievedUser generated.User
				err := json.Unmarshal(rec.Body.Bytes(), &retrievedUser)
				require.NoError(t, err)
				assert.Equal(t, user.Id, retrievedUser.Id)
				assert.Equal(t, user.Email, retrievedUser.Email)
			}
		})
	}
}

func TestValidationModes_Integration(t *testing.T) {
	modes := []struct {
		name         string
		requestBody  string
		shouldPass   bool
		description  string
	}{
		{
			name:        "default",
			requestBody: `{"email": "default@example.com", "age": 25, "extra": "should_fail"}`,
			shouldPass:  false,
			description: "Default mode should reject additional properties",
		},
		{
			name:        "flexible",
			requestBody: `{"email": "flexible@example.com", "age": 25, "hobby": "reading", "score": 95}`,
			shouldPass:  true,
			description: "Flexible mode should accept additional properties",
		},
		{
			name:        "strict",
			requestBody: `{"email": "strict@example.com", "age": 25}`,
			shouldPass:  true,
			description: "Strict mode should accept valid defined properties",
		},
		{
			name:        "strict",
			requestBody: `{"email": "strict2@example.com", "age": 25, "extra": "should_fail"}`,
			shouldPass:  false,
			description: "Strict mode should reject additional properties",
		},
	}

	for _, mode := range modes {
		t.Run(mode.name+"_"+mode.description, func(t *testing.T) {
			e, _, _ := setupTestAppVariants(t, mode.name)

			req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(mode.requestBody))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if mode.shouldPass {
				assert.Equal(t, http.StatusCreated, rec.Code, "Expected success for: %s", mode.description)
				var user generated.User
				err := json.Unmarshal(rec.Body.Bytes(), &user)
				require.NoError(t, err)
				assert.NotZero(t, user.Id)
			} else {
				assert.NotEqual(t, http.StatusCreated, rec.Code, "Expected failure for: %s", mode.description)
			}
		})
	}
}

func TestDatabaseUserHandler_JobEnqueuing(t *testing.T) {
	e, _, db := setupTestAppVariants(t, "default")

	// Create a user (should trigger job enqueuing)
	reqBody := `{"email": "job-test@example.com", "age": 32, "name": "Job Test User"}`
	req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	// Verify user was created
	var user generated.User
	err := json.Unmarshal(rec.Body.Bytes(), &user)
	require.NoError(t, err)

	// Check if job was enqueued (this would require accessing the job queue)
	// For now, we'll just verify the user creation was successful
	assert.Equal(t, "job-test@example.com", string(user.Email))
	assert.Equal(t, 32, user.Age)
}

func TestDatabaseUserHandler_UniqueEmailConstraint(t *testing.T) {
	e, _, _ := setupTestAppVariants(t, "default")

	// Create first user
	reqBody := `{"email": "unique@example.com", "age": 25}`
	req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)

	// Try to create second user with same email
	req2 := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(reqBody))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()

	e.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusInternalServerError, rec2.Code)
	assert.Contains(t, rec2.Body.String(), "UNIQUE constraint failed")
}