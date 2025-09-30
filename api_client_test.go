package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"openapi-validation-example/generated"

	types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// APIClient provides a test client for making HTTP requests
type APIClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *APIClient) CreateUser(user generated.UserRequest) (*generated.User, *http.Response, error) {
	jsonData, err := json.Marshal(user)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.HTTPClient.Post(
		c.BaseURL+"/users",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return nil, resp, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp, err
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, resp, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var createdUser generated.User
	err = json.Unmarshal(body, &createdUser)
	if err != nil {
		return nil, resp, err
	}

	return &createdUser, resp, nil
}

func (c *APIClient) CreateUserRaw(data map[string]interface{}) (*http.Response, []byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.HTTPClient.Post(
		c.BaseURL+"/users",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return resp, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}

	return resp, body, nil
}

func (c *APIClient) GetUser(id int64) (*generated.User, *http.Response, error) {
	url := fmt.Sprintf("%s/users/%d", c.BaseURL, id)
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, resp, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var user generated.User
	err = json.Unmarshal(body, &user)
	if err != nil {
		return nil, resp, err
	}

	return &user, resp, nil
}

func (c *APIClient) GetUserRaw(id string) (*http.Response, []byte, error) {
	url := fmt.Sprintf("%s/users/%s", c.BaseURL, id)
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return resp, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}

	return resp, body, nil
}

// TestAPIClient tests the API client against running servers
// Note: This requires servers to be running on the specified ports
func TestAPIClient_CreateAndGetUser(t *testing.T) {
	// Skip this test if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	tests := []struct {
		name    string
		baseURL string
		mode    string
	}{
		{
			name:    "In-Memory Server",
			baseURL: "http://localhost:8091",
			mode:    "memory",
		},
		{
			name:    "Database Server",
			baseURL: "http://localhost:8090",
			mode:    "database",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewAPIClient(tt.baseURL)

			// Test data
			namePtr := stringPtr("API Test User")
			bioPtr := stringPtr("Testing API client")
			isActivePtr := boolPtr(true)

			userReq := generated.UserRequest{
				Email:    "api-test@example.com",
				Age:      25,
				Name:     namePtr,
				Bio:      bioPtr,
				IsActive: isActivePtr,
			}

			// Create user
			createdUser, createResp, err := client.CreateUser(userReq)
			if err != nil {
				t.Skipf("Server not running on %s: %v", tt.baseURL, err)
			}
			require.NoError(t, err)
			assert.Equal(t, http.StatusCreated, createResp.StatusCode)
			assert.NotZero(t, createdUser.Id)
			assert.Equal(t, userReq.Email, createdUser.Email)
			assert.Equal(t, userReq.Age, createdUser.Age)

			// Get user
			retrievedUser, getResp, err := client.GetUser(createdUser.Id)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, getResp.StatusCode)
			assert.Equal(t, createdUser.Id, retrievedUser.Id)
			assert.Equal(t, createdUser.Email, retrievedUser.Email)
		})
	}
}

func TestAPIClient_ValidationModes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	tests := []struct {
		name              string
		baseURL           string
		validationMode    string
		requestData       map[string]interface{}
		expectSuccess     bool
		expectedStatus    int
	}{
		{
			name:           "Flexible mode with extra properties",
			baseURL:        "http://localhost:8081", // Assuming flexible mode runs here
			validationMode: "flexible",
			requestData: map[string]interface{}{
				"email":    "flexible-test@example.com",
				"age":      30,
				"hobby":    "programming",
				"location": "Tokyo",
			},
			expectSuccess:  true,
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "Strict mode with extra properties",
			baseURL:        "http://localhost:8082", // Assuming strict mode runs here
			validationMode: "strict",
			requestData: map[string]interface{}{
				"email":      "strict-test@example.com",
				"age":        30,
				"extra_prop": "should_fail",
			},
			expectSuccess:  false,
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewAPIClient(tt.baseURL)

			resp, body, err := client.CreateUserRaw(tt.requestData)
			if err != nil {
				t.Skipf("Server not running on %s: %v", tt.baseURL, err)
			}

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			if tt.expectSuccess {
				var user generated.User
				err = json.Unmarshal(body, &user)
				require.NoError(t, err)
				assert.NotZero(t, user.Id)
			} else {
				// Should contain error message
				var errorResp map[string]interface{}
				err = json.Unmarshal(body, &errorResp)
				require.NoError(t, err)
				assert.Contains(t, errorResp, "error")
			}
		})
	}
}

func TestAPIClient_ErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	client := NewAPIClient("http://localhost:8091")

	tests := []struct {
		name         string
		requestData  map[string]interface{}
		expectedCode int
	}{
		{
			name: "Missing required field - email",
			requestData: map[string]interface{}{
				"age": 25,
			},
			expectedCode: http.StatusInternalServerError,
		},
		{
			name: "Missing required field - age",
			requestData: map[string]interface{}{
				"email": "missing-age@example.com",
			},
			expectedCode: http.StatusInternalServerError,
		},
		{
			name: "Invalid email format",
			requestData: map[string]interface{}{
				"email": "not-an-email",
				"age":   25,
			},
			expectedCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, _, err := client.CreateUserRaw(tt.requestData)
			if err != nil {
				t.Skipf("Server not running: %v", err)
			}

			assert.Equal(t, tt.expectedCode, resp.StatusCode)
		})
	}
}

func TestAPIClient_GetUserErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	client := NewAPIClient("http://localhost:8091")

	tests := []struct {
		name         string
		userID       string
		expectedCode int
	}{
		{
			name:         "Non-existent user",
			userID:       "999",
			expectedCode: http.StatusNotFound,
		},
		{
			name:         "Invalid user ID format",
			userID:       "invalid",
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Zero user ID",
			userID:       "0",
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, _, err := client.GetUserRaw(tt.userID)
			if err != nil {
				t.Skipf("Server not running: %v", err)
			}

			assert.Equal(t, tt.expectedCode, resp.StatusCode)
		})
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

// Benchmark tests
func BenchmarkAPIClient_CreateUser(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark")
	}

	client := NewAPIClient("http://localhost:8091")

	userReq := generated.UserRequest{
		Email: "benchmark@example.com",
		Age:   25,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create unique email for each request
		email := fmt.Sprintf("benchmark-%d@example.com", i)
		userReq.Email = types.Email(email)

		_, _, err := client.CreateUser(userReq)
		if err != nil {
			b.Skipf("Server not running: %v", err)
		}
	}
}

func BenchmarkAPIClient_GetUser(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark")
	}

	client := NewAPIClient("http://localhost:8091")

	// Create a test user first
	userReq := generated.UserRequest{
		Email: "get-benchmark@example.com",
		Age:   25,
	}

	user, _, err := client.CreateUser(userReq)
	if err != nil {
		b.Skipf("Server not running: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := client.GetUser(user.Id)
		if err != nil {
			b.Fatalf("Failed to get user: %v", err)
		}
	}
}