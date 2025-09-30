package handlers

import (
	"net/http"

	"openapi-validation-example/generated"
	"openapi-validation-example/pkg/database"

	"github.com/labstack/echo/v4"
)

// InMemoryUserHandler implements the generated.ServerInterface (in-memory version)
type InMemoryUserHandler struct {
	Users  map[int64]generated.User
	NextID int64
}

func NewInMemoryUserHandler() *InMemoryUserHandler {
	return &InMemoryUserHandler{
		Users:  make(map[int64]generated.User),
		NextID: 1,
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
		Id:    h.NextID,
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

	h.Users[h.NextID] = user
	h.NextID++

	return ctx.JSON(http.StatusCreated, user)
}

// GetUserById implements the generated.ServerInterface.GetUserById method
func (h *InMemoryUserHandler) GetUserById(ctx echo.Context, id int64) error {
	user, exists := h.Users[id]
	if !exists {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": "User not found",
		})
	}

	return ctx.JSON(http.StatusOK, user)
}

// UserHandler implements the generated.ServerInterface (database version)
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
	var req generated.UserRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON format",
		})
	}

	// Extract additional properties (properties not defined in UserRequest)
	var rawData map[string]interface{}
	if err := ctx.Bind(&rawData); err == nil {
		// Remove known fields
		delete(rawData, "email")
		delete(rawData, "age")
		delete(rawData, "name")
		delete(rawData, "bio")
		delete(rawData, "is_active")

		// Create user with additional properties
		user, err := h.db.CreateUser(req, rawData)
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
		}

		return ctx.JSON(http.StatusCreated, user)
	}

	// Fallback: create without additional properties
	user, err := h.db.CreateUser(req, nil)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}

	return ctx.JSON(http.StatusCreated, user)
}

// GetUserById implements the generated.ServerInterface.GetUserById method
func (h *UserHandler) GetUserById(ctx echo.Context, id int64) error {
	user, err := h.db.GetUserByID(id)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": "User not found",
		})
	}

	return ctx.JSON(http.StatusOK, user)
}