package main

import (
	"openapi-validation-example/pkg/validation"

	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationMiddleware_NewValidationMiddleware(t *testing.T) {
	tests := []struct {
		name        string
		specFile    string
		expectError bool
	}{
		{
			name:        "Valid default spec",
			specFile:    "openapi.yaml",
			expectError: false,
		},
		{
			name:        "Valid flexible spec",
			specFile:    "openapi-flexible.yaml",
			expectError: false,
		},
		{
			name:        "Valid strict spec",
			specFile:    "openapi-strict.yaml",
			expectError: false,
		},
		{
			name:        "Non-existent spec file",
			specFile:    "non-existent.yaml",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware, err := validation.NewValidationMiddleware(tt.specFile)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, middleware)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, middleware)
				// router is not exported
			}
		})
	}
}

func TestValidationMiddleware_Validate(t *testing.T) {
	middleware, err := validation.NewValidationMiddleware("openapi.yaml")
	require.NoError(t, err)

	e := echo.New()
	e.Use(middleware.Validate())

	// Add a dummy handler
	e.POST("/users", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	tests := []struct {
		name           string
		method         string
		path           string
		body           string
		contentType    string
		expectedStatus int
		expectError    bool
	}{
		{
			name:           "Valid POST request",
			method:         http.MethodPost,
			path:           "/users",
			body:           `{"email": "valid@example.com", "age": 25}`,
			contentType:    "application/json",
			expectedStatus: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Invalid POST request - missing email",
			method:         http.MethodPost,
			path:           "/users",
			body:           `{"age": 25}`,
			contentType:    "application/json",
			expectedStatus: http.StatusInternalServerError,
			expectError:    true,
		},
		{
			name:           "Invalid POST request - missing age",
			method:         http.MethodPost,
			path:           "/users",
			body:           `{"email": "test@example.com"}`,
			contentType:    "application/json",
			expectedStatus: http.StatusInternalServerError,
			expectError:    true,
		},
		{
			name:           "Invalid POST request - bad email format",
			method:         http.MethodPost,
			path:           "/users",
			body:           `{"email": "not-an-email", "age": 25}`,
			contentType:    "application/json",
			expectedStatus: http.StatusInternalServerError,
			expectError:    true,
		},
		{
			name:           "Invalid POST request - negative age",
			method:         http.MethodPost,
			path:           "/users",
			body:           `{"email": "test@example.com", "age": -1}`,
			contentType:    "application/json",
			expectedStatus: http.StatusInternalServerError,
			expectError:    true,
		},
		{
			name:           "Invalid JSON",
			method:         http.MethodPost,
			path:           "/users",
			body:           `{"email": "test@example.com", "age": }`,
			contentType:    "application/json",
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
		},
		{
			name:           "Non-existent route",
			method:         http.MethodPost,
			path:           "/non-existent",
			body:           `{"test": "data"}`,
			contentType:    "application/json",
			expectedStatus: http.StatusNotFound,
			expectError:    false, // Middleware passes through for non-matched routes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			if tt.contentType != "" {
				req.Header.Set(echo.HeaderContentType, tt.contentType)
			}
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)

			if tt.expectError {
				// Should contain error message in response
				responseBody := rec.Body.String()
				if tt.expectedStatus == http.StatusBadRequest {
					assert.Contains(t, responseBody, "validation failed")
				} else if tt.expectedStatus == http.StatusInternalServerError {
					assert.Contains(t, responseBody, "validation failed")
				}
			}
		})
	}
}

func TestValidationMiddleware_FlexibleMode(t *testing.T) {
	middleware, err := validation.NewValidationMiddleware("openapi-flexible.yaml")
	require.NoError(t, err)

	e := echo.New()
	e.Use(middleware.Validate())

	e.POST("/users", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	tests := []struct {
		name           string
		body           string
		expectedStatus int
		description    string
	}{
		{
			name:           "Valid with additional properties",
			body:           `{"email": "flexible@example.com", "age": 25, "hobby": "programming", "location": "Tokyo"}`,
			expectedStatus: http.StatusOK,
			description:    "Should allow additional properties in flexible mode",
		},
		{
			name:           "Valid with only required fields",
			body:           `{"email": "minimal@example.com", "age": 30}`,
			expectedStatus: http.StatusOK,
			description:    "Should work with minimal required fields",
		},
		{
			name:           "Invalid missing email",
			body:           `{"age": 25, "extra": "property"}`,
			expectedStatus: http.StatusInternalServerError,
			description:    "Should still require email field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(tt.body))
			req.Header.Set(echo.HeaderContentType, "application/json")
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code, tt.description)
		})
	}
}

func TestValidationMiddleware_StrictMode(t *testing.T) {
	middleware, err := validation.NewValidationMiddleware("openapi-strict.yaml")
	require.NoError(t, err)

	e := echo.New()
	e.Use(middleware.Validate())

	e.POST("/users", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	tests := []struct {
		name           string
		body           string
		expectedStatus int
		description    string
	}{
		{
			name:           "Valid with defined properties only",
			body:           `{"email": "strict@example.com", "age": 25, "name": "Test User"}`,
			expectedStatus: http.StatusOK,
			description:    "Should allow defined properties in strict mode",
		},
		{
			name:           "Invalid with additional properties",
			body:           `{"email": "strict@example.com", "age": 25, "extra": "property"}`,
			expectedStatus: http.StatusInternalServerError,
			description:    "Should reject additional properties in strict mode",
		},
		{
			name:           "Valid minimal",
			body:           `{"email": "minimal@example.com", "age": 30}`,
			expectedStatus: http.StatusOK,
			description:    "Should work with minimal required fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(tt.body))
			req.Header.Set(echo.HeaderContentType, "application/json")
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code, tt.description)
		})
	}
}

func TestValidationMiddleware_GetUserValidation(t *testing.T) {
	middleware, err := validation.NewValidationMiddleware("openapi.yaml")
	require.NoError(t, err)

	e := echo.New()
	e.Use(middleware.Validate())

	e.GET("/users/:id", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		description    string
	}{
		{
			name:           "Valid numeric ID",
			path:           "/users/123",
			expectedStatus: http.StatusOK,
			description:    "Should accept valid numeric user ID",
		},
		{
			name:           "Valid ID with leading zero",
			path:           "/users/0123",
			expectedStatus: http.StatusOK,
			description:    "Should accept numeric ID with leading zero",
		},
		{
			name:           "Invalid non-numeric ID",
			path:           "/users/invalid",
			expectedStatus: http.StatusBadRequest,
			description:    "Should reject non-numeric user ID",
		},
		{
			name:           "Invalid negative ID",
			path:           "/users/-1",
			expectedStatus: http.StatusBadRequest,
			description:    "Should reject negative user ID",
		},
		{
			name:           "Invalid zero ID",
			path:           "/users/0",
			expectedStatus: http.StatusBadRequest,
			description:    "Should reject zero user ID (minimum is 1)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code, tt.description)
		})
	}
}

func TestValidationMiddleware_ContentTypeValidation(t *testing.T) {
	middleware, err := validation.NewValidationMiddleware("openapi.yaml")
	require.NoError(t, err)

	e := echo.New()
	e.Use(middleware.Validate())

	e.POST("/users", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	tests := []struct {
		name           string
		contentType    string
		body           string
		expectedStatus int
		description    string
	}{
		{
			name:           "Valid JSON content type",
			contentType:    "application/json",
			body:           `{"email": "test@example.com", "age": 25}`,
			expectedStatus: http.StatusOK,
			description:    "Should accept application/json",
		},
		{
			name:           "Valid JSON with charset",
			contentType:    "application/json; charset=utf-8",
			body:           `{"email": "test@example.com", "age": 25}`,
			expectedStatus: http.StatusOK,
			description:    "Should accept application/json with charset",
		},
		{
			name:           "Invalid content type",
			contentType:    "text/plain",
			body:           `{"email": "test@example.com", "age": 25}`,
			expectedStatus: http.StatusBadRequest,
			description:    "Should reject non-JSON content type",
		},
		{
			name:           "Missing content type",
			contentType:    "",
			body:           `{"email": "test@example.com", "age": 25}`,
			expectedStatus: http.StatusBadRequest,
			description:    "Should require content type for POST with body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(tt.body))
			if tt.contentType != "" {
				req.Header.Set(echo.HeaderContentType, tt.contentType)
			}
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code, tt.description)
		})
	}
}

func TestValidationMiddleware_EdgeCases(t *testing.T) {
	middleware, err := validation.NewValidationMiddleware("openapi.yaml")
	require.NoError(t, err)

	e := echo.New()
	e.Use(middleware.Validate())

	e.POST("/users", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	tests := []struct {
		name           string
		body           string
		expectedStatus int
		description    string
	}{
		{
			name:           "Empty body",
			body:           "",
			expectedStatus: http.StatusBadRequest,
			description:    "Should reject empty request body",
		},
		{
			name:           "Empty JSON object",
			body:           "{}",
			expectedStatus: http.StatusInternalServerError,
			description:    "Should reject JSON object without required fields",
		},
		{
			name:           "Large valid request",
			body:           `{"email": "large@example.com", "age": 25, "bio": "` + generateLongString(400) + `"}`,
			expectedStatus: http.StatusOK,
			description:    "Should handle large valid requests",
		},
		{
			name:           "Bio too long",
			body:           `{"email": "toolong@example.com", "age": 25, "bio": "` + generateLongString(600) + `"}`,
			expectedStatus: http.StatusInternalServerError,
			description:    "Should reject bio longer than 500 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(tt.body))
			req.Header.Set(echo.HeaderContentType, "application/json")
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code, tt.description)
		})
	}
}

// Helper function to generate long strings for testing
func generateLongString(length int) string {
	result := make([]byte, length)
	for i := range result {
		result[i] = 'a' + byte(i%26)
	}
	return string(result)
}

// Benchmark validation performance
func BenchmarkValidationMiddleware_ValidRequest(b *testing.B) {
	middleware, err := validation.NewValidationMiddleware("openapi.yaml")
	require.NoError(b, err)

	e := echo.New()
	e.Use(middleware.Validate())
	e.POST("/users", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	body := `{"email": "benchmark@example.com", "age": 25, "name": "Benchmark User"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(body))
		req.Header.Set(echo.HeaderContentType, "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)
	}
}

func BenchmarkValidationMiddleware_InvalidRequest(b *testing.B) {
	middleware, err := validation.NewValidationMiddleware("openapi.yaml")
	require.NoError(b, err)

	e := echo.New()
	e.Use(middleware.Validate())
	e.POST("/users", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	body := `{"age": 25}` // Missing required email field

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(body))
		req.Header.Set(echo.HeaderContentType, "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)
	}
}