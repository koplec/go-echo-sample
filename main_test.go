package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"openapi-validation-example/generated"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestApp creates a test Echo app with in-memory InMemoryUserHandler
func setupTestApp(t *testing.T) (*echo.Echo, *InMemoryUserHandler) {
	e := echo.New()

	// Setup validation middleware
	validationMiddleware, err := NewValidationMiddleware("openapi.yaml")
	require.NoError(t, err)
	e.Use(validationMiddleware.Validate())

	// Create handler
	userHandler := NewInMemoryUserHandler()

	// Register routes
	generated.RegisterHandlers(e, userHandler)

	return e, userHandler
}

func TestInMemoryUserHandler_CreateUser(t *testing.T) {
	e, _ := setupTestApp(t)

	tests := []struct {
		name           string
		requestBody    string
		expectedStatus int
		checkResponse  func(t *testing.T, body string)
	}{
		{
			name:           "Valid user with all fields",
			requestBody:    `{"email": "test@example.com", "age": 25, "name": "Test User", "bio": "Test bio", "is_active": true}`,
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, body string) {
				var user generated.User
				err := json.Unmarshal([]byte(body), &user)
				require.NoError(t, err)
				assert.Equal(t, "test@example.com", string(user.Email))
				assert.Equal(t, 25, user.Age)
				assert.Equal(t, int64(1), user.Id)
				assert.NotNil(t, user.Name)
				assert.Equal(t, "Test User", *user.Name)
			},
		},
		{
			name:           "Valid user with required fields only",
			requestBody:    `{"email": "minimal@example.com", "age": 30}`,
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, body string) {
				var user generated.User
				err := json.Unmarshal([]byte(body), &user)
				require.NoError(t, err)
				assert.Equal(t, "minimal@example.com", string(user.Email))
				assert.Equal(t, 30, user.Age)
				assert.Equal(t, int64(1), user.Id)
			},
		},
		{
			name:           "Invalid - missing email",
			requestBody:    `{"age": 25}`,
			expectedStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, "error")
			},
		},
		{
			name:           "Invalid - missing age",
			requestBody:    `{"email": "noage@example.com"}`,
			expectedStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, "error")
			},
		},
		{
			name:           "Invalid - bad email format",
			requestBody:    `{"email": "not-an-email", "age": 25}`,
			expectedStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, "error")
			},
		},
		{
			name:           "Invalid - negative age",
			requestBody:    `{"email": "negative@example.com", "age": -5}`,
			expectedStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, "error")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(tt.requestBody))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, rec.Body.String())
			}
		})
	}
}

func TestInMemoryUserHandler_GetUserById(t *testing.T) {
	e, userHandler := setupTestApp(t)

	// Create a test user first
	testUser := generated.User{
		Id:    1,
		Email: "get-test@example.com",
		Age:   28,
	}
	userHandler.users[1] = testUser
	userHandler.nextID = 2

	tests := []struct {
		name           string
		userID         string
		expectedStatus int
		checkResponse  func(t *testing.T, body string)
	}{
		{
			name:           "Get existing user",
			userID:         "1",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, body string) {
				var user generated.User
				err := json.Unmarshal([]byte(body), &user)
				require.NoError(t, err)
				assert.Equal(t, "get-test@example.com", string(user.Email))
				assert.Equal(t, 28, user.Age)
				assert.Equal(t, int64(1), user.Id)
			},
		},
		{
			name:           "Get non-existing user",
			userID:         "999",
			expectedStatus: http.StatusNotFound,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, "User not found")
			},
		},
		{
			name:           "Invalid user ID format",
			userID:         "invalid",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, "Invalid format for parameter id")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/users/"+tt.userID, nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, rec.Body.String())
			}
		})
	}
}

func TestInMemoryUserHandler_Integration(t *testing.T) {
	e, _ := setupTestApp(t)

	// Test creating and retrieving users
	t.Run("Create and Get User Flow", func(t *testing.T) {
		// Create user
		createReq := httptest.NewRequest(http.MethodPost, "/users",
			bytes.NewBufferString(`{"email": "integration@example.com", "age": 35, "name": "Integration Test User"}`))
		createReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		createRec := httptest.NewRecorder()

		e.ServeHTTP(createRec, createReq)

		assert.Equal(t, http.StatusCreated, createRec.Code)

		var createdUser generated.User
		err := json.Unmarshal(createRec.Body.Bytes(), &createdUser)
		require.NoError(t, err)

		// Get user
		getReq := httptest.NewRequest(http.MethodGet, "/users/1", nil)
		getRec := httptest.NewRecorder()

		e.ServeHTTP(getRec, getReq)

		assert.Equal(t, http.StatusOK, getRec.Code)

		var retrievedUser generated.User
		err = json.Unmarshal(getRec.Body.Bytes(), &retrievedUser)
		require.NoError(t, err)

		// Verify they match
		assert.Equal(t, createdUser.Id, retrievedUser.Id)
		assert.Equal(t, createdUser.Email, retrievedUser.Email)
		assert.Equal(t, createdUser.Age, retrievedUser.Age)
		assert.Equal(t, createdUser.Name, retrievedUser.Name)
	})

	t.Run("Multiple Users", func(t *testing.T) {
		users := []struct {
			email string
			age   int
			name  string
		}{
			{"user1@example.com", 25, "User One"},
			{"user2@example.com", 30, "User Two"},
			{"user3@example.com", 35, "User Three"},
		}

		// Create multiple users
		for i, user := range users {
			reqBody := map[string]interface{}{
				"email": user.email,
				"age":   user.age,
				"name":  user.name,
			}
			jsonBody, _ := json.Marshal(reqBody)

			req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBuffer(jsonBody))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusCreated, rec.Code)

			var createdUser generated.User
			err := json.Unmarshal(rec.Body.Bytes(), &createdUser)
			require.NoError(t, err)
			assert.Equal(t, int64(i+1), createdUser.Id)
		}

		// Retrieve and verify each user
		for i, user := range users {
			req := httptest.NewRequest(http.MethodGet, "/users/"+string(rune(i+1+48)), nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)

			var retrievedUser generated.User
			err := json.Unmarshal(rec.Body.Bytes(), &retrievedUser)
			require.NoError(t, err)
			assert.Equal(t, user.email, string(retrievedUser.Email))
		}
	})
}