package main

import (
	"fmt"
	"net/http"
	"os"

	"openapi-validation-example/generated"
	"openapi-validation-example/pkg/validation"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// InMemoryUserHandler implements the generated.ServerInterface (in-memory version)
type InMemoryUserHandler struct {
	users  map[int64]generated.User
	nextID int64
}

func NewInMemoryUserHandler() *InMemoryUserHandler {
	return &InMemoryUserHandler{
		users:  make(map[int64]generated.User),
		nextID: 1,
	}
}

// CreateUser implements the generated.ServerInterface.CreateUser method
func (h *InMemoryUserHandler) CreateUser(ctx echo.Context) error {
	var req generated.UserRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON format",
		})
	}

	user := generated.User{
		Id:    h.nextID,
		Email: req.Email,
		Age:   req.Age,
	}

	// Handle optional fields
	if req.Name != nil {
		user.Name = req.Name
	}
	if req.Bio != nil {
		user.Bio = req.Bio
	}
	if req.IsActive != nil {
		user.IsActive = req.IsActive
	}

	h.users[h.nextID] = user
	h.nextID++

	return ctx.JSON(http.StatusCreated, user)
}

// GetUserById implements the generated.ServerInterface.GetUserById method
func (h *InMemoryUserHandler) GetUserById(ctx echo.Context, id int64) error {
	user, exists := h.users[id]
	if !exists {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": "User not found",
		})
	}

	return ctx.JSON(http.StatusOK, user)
}

func main() {
	e := echo.New()

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	validationMiddleware, err := validation.NewValidationMiddleware("openapi.yaml")
	if err != nil {
		e.Logger.Fatal("Failed to initialize validation middleware:", err)
	}

	e.Use(validationMiddleware.Validate())

	userHandler := NewInMemoryUserHandler()

	// Use the generated RegisterHandlers function to register routes
	generated.RegisterHandlers(e, userHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Server starting on :%s\n", port)
	fmt.Printf("API Documentation: http://localhost:%s\n", port)
	fmt.Println("Test with: make test")

	if err := e.Start(":" + port); err != nil {
		e.Logger.Fatal("Server failed to start:", err)
	}
}