.PHONY: generate install clean run

generate:
	@echo "Generating code from OpenAPI spec..."
	@mkdir -p generated
	oapi-codegen -package generated -generate types openapi.yaml > generated/types.go
	oapi-codegen -package generated -generate echo openapi.yaml > generated/server.go
	@echo "Generating database code with sqlc..."
	sqlc generate

install:
	@echo "Installing dependencies..."
	go mod tidy
	go install github.com/deepmap/oapi-codegen/cmd/oapi-codegen@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

clean:
	@echo "Cleaning generated files..."
	rm -rf generated/ db/
	rm -f *.db

run:
	@echo "Running server (default validation)..."
	go run main-variants.go validator.go database.go

run-flexible:
	@echo "Running server (flexible validation - accepts any JSON)..."
	VALIDATION_MODE=flexible go run main-variants.go validator.go database.go

run-strict:
	@echo "Running server (strict validation - rejects undefined properties)..."
	VALIDATION_MODE=strict go run main-variants.go validator.go database.go

test:
	@echo "Testing default validation mode..."
	@echo "1. Valid user with all properties:"
	curl -X POST http://localhost:8080/users \
		-H "Content-Type: application/json" \
		-d '{"email": "user1@example.com", "age": 25, "name": "John Doe", "bio": "Software engineer", "is_active": true}'
	@echo ""
	@echo "2. Valid user with only required properties:"
	curl -X POST http://localhost:8080/users \
		-H "Content-Type: application/json" \
		-d '{"email": "user2@example.com", "age": 30}'
	@echo ""
	@echo "3. Invalid - missing email:"
	curl -X POST http://localhost:8080/users \
		-H "Content-Type: application/json" \
		-d '{"age": 25}'
	@echo ""
	@echo "4. Invalid - negative age:"
	curl -X POST http://localhost:8080/users \
		-H "Content-Type: application/json" \
		-d '{"email": "user3@example.com", "age": -5}'
	@echo ""

test-flexible:
	@echo "Testing flexible validation mode (accepts additional properties)..."
	@echo "1. Valid user with extra properties:"
	curl -X POST http://localhost:8080/users \
		-H "Content-Type: application/json" \
		-d '{"email": "flexible@example.com", "age": 28, "hobby": "reading", "location": "Tokyo", "score": 95}'
	@echo ""

test-strict:
	@echo "Testing strict validation mode (rejects additional properties)..."
	@echo "1. Valid user (defined properties only):"
	curl -X POST http://localhost:8080/users \
		-H "Content-Type: application/json" \
		-d '{"email": "strict@example.com", "age": 32, "name": "Jane Doe"}'
	@echo ""
	@echo "2. Invalid - has additional property:"
	curl -X POST http://localhost:8080/users \
		-H "Content-Type: application/json" \
		-d '{"email": "strict2@example.com", "age": 28, "extra_field": "should_fail"}'
	@echo ""