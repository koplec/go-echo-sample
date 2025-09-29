package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"openapi-validation-example/generated"
	"openapi-validation-example/pkg/database"
	"openapi-validation-example/pkg/jobs"
	"openapi-validation-example/pkg/validation"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// UserHandler implements the generated.ServerInterface
type UserHandler struct {
	db *database.DatabaseService
}

func NewUserHandler(db *database.DatabaseService) *UserHandler {
	return &UserHandler{
		db: db,
	}
}

// CreateUser implements the generated.ServerInterface.CreateUser method
func (h *UserHandler) CreateUser(ctx echo.Context) error {
	var rawBody map[string]interface{}
	if err := ctx.Bind(&rawBody); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON format",
		})
	}

	var userReq generated.UserRequest
	reqBytes, _ := json.Marshal(rawBody)
	if err := json.Unmarshal(reqBytes, &userReq); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid request format",
		})
	}

	knownFields := map[string]bool{
		"email":     true,
		"age":       true,
		"name":      true,
		"bio":       true,
		"is_active": true,
	}

	additionalProps := make(map[string]interface{})
	for key, value := range rawBody {
		if !knownFields[key] {
			additionalProps[key] = value
		}
	}

	user, err := h.db.CreateUser(userReq, additionalProps)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to create user: %v", err),
		})
	}

	return ctx.JSON(http.StatusCreated, user)
}

// GetUserById implements the generated.ServerInterface.GetUserById method
func (h *UserHandler) GetUserById(ctx echo.Context, id int64) error {
	user, err := h.db.GetUserByID(id)
	if err != nil {
		if err.Error() == "user not found" {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "User not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get user: %v", err),
		})
	}

	return ctx.JSON(http.StatusOK, user)
}

func createApp(validationMode string) (*echo.Echo, error) {
	e := echo.New()

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	var specFile string
	switch validationMode {
	case "flexible":
		specFile = "openapi-flexible.yaml"
	case "strict":
		specFile = "openapi-strict.yaml"
	default:
		specFile = "openapi.yaml"
	}

	validationMiddleware, err := validation.NewValidationMiddleware(specFile)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize validation middleware: %w", err)
	}

	e.Use(validationMiddleware.Validate())

	db, err := database.NewDatabaseService("users.db")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	userHandler := NewUserHandler(db)

	// Use the generated RegisterHandlers function to register routes
	generated.RegisterHandlers(e, userHandler)

	return e, nil
}

func main() {
	validationMode := os.Getenv("VALIDATION_MODE")
	if validationMode == "" {
		validationMode = "default"
	}

	e, err := createApp(validationMode)
	if err != nil {
		log.Fatal("Failed to create app:", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Server starting on :%s\n", port)
	fmt.Printf("Validation mode: %s\n", validationMode)
	fmt.Println("Available modes:")
	fmt.Println("  VALIDATION_MODE=default  - Default validation with optional properties")
	fmt.Println("  VALIDATION_MODE=flexible - Accepts any additional JSON properties")
	fmt.Println("  VALIDATION_MODE=strict   - Rejects undefined properties")

	if err := e.Start(":" + port); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}