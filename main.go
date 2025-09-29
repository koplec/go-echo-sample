package main

import (
	"fmt"
	"net/http"
	"strconv"

	"openapi-validation-example/generated"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type UserService struct {
	users  map[int64]generated.User
	nextID int64
}

func NewUserService() *UserService {
	return &UserService{
		users:  make(map[int64]generated.User),
		nextID: 1,
	}
}

func (s *UserService) CreateUser(ctx echo.Context) error {
	var req generated.UserRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON format",
		})
	}

	user := generated.User{
		Id:    s.nextID,
		Email: req.Email,
		Age:   req.Age,
	}

	s.users[s.nextID] = user
	s.nextID++

	return ctx.JSON(http.StatusCreated, user)
}

func (s *UserService) GetUserById(ctx echo.Context, id int64) error {
	user, exists := s.users[id]
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

	validationMiddleware, err := NewValidationMiddleware("openapi.yaml")
	if err != nil {
		e.Logger.Fatal("Failed to initialize validation middleware:", err)
	}

	e.Use(validationMiddleware.Validate())

	userService := NewUserService()

	e.POST("/users", func(c echo.Context) error {
		return userService.CreateUser(c)
	})

	e.GET("/users/:id", func(c echo.Context) error {
		idStr := c.Param("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "Invalid user ID format",
			})
		}
		return userService.GetUserById(c, id)
	})

	fmt.Println("Server starting on :8080")
	fmt.Println("API Documentation: http://localhost:8080")
	fmt.Println("Test with: make test")

	if err := e.Start(":8080"); err != nil {
		e.Logger.Fatal("Server failed to start:", err)
	}
}