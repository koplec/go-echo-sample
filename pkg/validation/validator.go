package validation

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/labstack/echo/v4"
)

type ValidationMiddleware struct {
	router routers.Router
}

func NewValidationMiddleware(specPath string) (*ValidationMiddleware, error) {
	ctx := context.Background()
	loader := &openapi3.Loader{Context: ctx, IsExternalRefsAllowed: true}
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load OpenAPI spec: %w", err)
	}

	if err := doc.Validate(ctx); err != nil {
		return nil, fmt.Errorf("OpenAPI spec validation failed: %w", err)
	}

	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		return nil, fmt.Errorf("failed to create router: %w", err)
	}

	return &ValidationMiddleware{
		router: router,
	}, nil
}

func (v *ValidationMiddleware) Validate() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()

			route, pathParams, err := v.router.FindRoute(req)
			if err != nil {
				return next(c)
			}

			requestValidationInput := &openapi3filter.RequestValidationInput{
				Request:    req,
				PathParams: pathParams,
				Route:      route,
			}

			ctx := context.Background()
			if err := openapi3filter.ValidateRequest(ctx, requestValidationInput); err != nil {
				return v.handleValidationError(c, err)
			}

			return next(c)
		}
	}
}

func (v *ValidationMiddleware) handleValidationError(c echo.Context, err error) error {
	var errorMessage string

	switch e := err.(type) {
	case *openapi3filter.RequestError:
		if e.Parameter != nil {
			errorMessage = fmt.Sprintf("Parameter validation failed for '%s': %s", e.Parameter.Name, e.Err.Error())
		} else if e.RequestBody != nil {
			errorMessage = fmt.Sprintf("Request body validation failed: %s", e.Err.Error())
		} else {
			errorMessage = fmt.Sprintf("Request validation failed: %s", e.Err.Error())
		}
	case *openapi3filter.SecurityRequirementsError:
		errorMessage = "Security requirements not met"
	default:
		errorMessage = err.Error()
	}

	errorMessage = v.formatErrorMessage(errorMessage)

	return c.JSON(http.StatusBadRequest, map[string]string{
		"error": errorMessage,
	})
}

func (v *ValidationMiddleware) formatErrorMessage(message string) string {
	message = strings.ReplaceAll(message, "doesn't match schema", "does not match the required format")
	message = strings.ReplaceAll(message, "Error at", "Error in field")
	message = strings.ReplaceAll(message, "Property", "Field")

	if strings.Contains(message, "minimum") {
		message = strings.ReplaceAll(message, "minimum", "must be at least")
	}

	if strings.Contains(message, "format") && strings.Contains(message, "email") {
		message = "Email address format is invalid"
	}

	if strings.Contains(message, "required") {
		message = strings.ReplaceAll(message, "property", "field")
	}

	return message
}