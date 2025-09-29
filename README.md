# OpenAPI Validation Example with Multiple Variants

This project demonstrates OpenAPI 3.0.3 specification validation in a Go Echo server using kin-openapi, oapi-codegen, and sqlc for database operations.

## Features

- **OpenAPI 3.0.3 Specification**: Complete API definition with User CRUD operations
- **Multiple Validation Modes**: Three different validation strategies
- **Code Generation**: Automatic type and server code generation with oapi-codegen
- **Database Integration**: SQLite database with sqlc-generated code
- **Request Validation**: Real-time validation using kin-openapi middleware
- **Optional Properties**: Support for required and optional user fields

## Project Structure

```
openapi-validation-example/
├── openapi.yaml           # Default OpenAPI spec (optional properties)
├── openapi-flexible.yaml # Flexible validation (additionalProperties: true)
├── openapi-strict.yaml   # Strict validation (additionalProperties: false)
├── go.mod                # Go module definition
├── Makefile             # Build and run commands
├── main-variants.go     # Echo server with multiple validation modes
├── validator.go         # kin-openapi validation middleware
├── database.go          # Database service layer
├── sqlc.yaml           # sqlc configuration
├── schema.sql          # Database schema
├── queries.sql         # SQL queries
├── generated/          # oapi-codegen generated code
│   ├── types.go        # Generated types
│   └── server.go       # Generated server interfaces
├── db/                 # sqlc generated code
│   ├── db.go          # Database connection
│   ├── models.go      # Database models
│   └── queries.sql.go # Generated query functions
└── README.md          # This file
```

## Getting Started

### Prerequisites

- Go 1.21 or later
- Make

### Installation

1. Install dependencies and tools:
```bash
make install
```

2. Generate code from OpenAPI specification and database code:
```bash
make generate
```

3. Run the server (choose validation mode):

**Default Mode** (optional properties allowed):
```bash
make run
```

**Flexible Mode** (accepts any additional JSON properties):
```bash
make run-flexible
```

**Strict Mode** (rejects undefined properties):
```bash
make run-strict
```

The server will start on `http://localhost:8080` and create a SQLite database file (`users.db`)

## Validation Modes

### 1. Default Mode (`openapi.yaml`)
- **Required fields**: `email`, `age`
- **Optional fields**: `name`, `bio`, `is_active`
- **Additional properties**: Not allowed (`additionalProperties: false`)
- **Use case**: Standard API with defined schema

### 2. Flexible Mode (`openapi-flexible.yaml`)
- **Required fields**: `email`, `age`
- **Optional fields**: `name`, `bio`, `is_active`
- **Additional properties**: Allowed (`additionalProperties: true`)
- **Use case**: APIs that need to accept dynamic/unknown fields

### 3. Strict Mode (`openapi-strict.yaml`)
- **Required fields**: `email`, `age`
- **Optional fields**: `name`, `bio`, `is_active`
- **Additional properties**: Explicitly forbidden
- **Use case**: APIs requiring exact schema compliance

## API Endpoints

### POST /users
Create a new user with validation based on the selected mode.

**Minimal Request Body:**
```json
{
  "email": "user@example.com",
  "age": 25
}
```

**Full Request Body:**
```json
{
  "email": "user@example.com",
  "age": 25,
  "name": "John Doe",
  "bio": "Software engineer",
  "is_active": true
}
```

**Validation Rules:**
- `email`: Required, must be valid email format
- `age`: Required, must be >= 0
- `name`: Optional, 1-100 characters
- `bio`: Optional, max 500 characters
- `is_active`: Optional, boolean (defaults to true)

### GET /users/{id}
Retrieve a user by ID.

**Parameters:**
- `id`: User ID (integer, >= 1)

## Testing Examples

### Default Mode Testing
```bash
make test
```

### Flexible Mode Testing (Accepts Additional Properties)
```bash
# Start flexible server in another terminal
make run-flexible

# Test with additional properties
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"email": "flexible@example.com", "age": 28, "hobby": "reading", "location": "Tokyo", "score": 95}'
```

**Response (201):**
```json
{
  "id": 1,
  "email": "flexible@example.com",
  "age": 28,
  "is_active": true
}
```

### Strict Mode Testing (Rejects Additional Properties)
```bash
# Start strict server in another terminal
make run-strict

# Test with additional properties (should fail)
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"email": "strict@example.com", "age": 32, "extra_field": "should_fail"}'
```

**Response (400):**
```json
{
  "error": "Request body validation failed: Additional property extra_field is not allowed"
}
```

### Common Test Cases

#### Valid User with All Properties
```bash
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"email": "complete@example.com", "age": 30, "name": "Jane Smith", "bio": "Product manager", "is_active": false}'
```

#### Missing Required Field (Email)
```bash
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"age": 25, "name": "No Email User"}'
```

#### Invalid Age (Negative)
```bash
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"email": "invalid@example.com", "age": -5}'
```

#### Invalid Email Format
```bash
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"email": "not-an-email", "age": 25}'
```

#### Get User
```bash
curl -X GET http://localhost:8080/users/1
```

## Quick Testing Commands

Test different validation modes:
```bash
make test           # Test default mode
make test-flexible  # Test flexible mode
make test-strict    # Test strict mode
```

## Implementation Details

### Database Layer
- **SQLite Database**: Persistent storage using modernc.org/sqlite driver
- **sqlc**: Type-safe SQL code generation
- **Schema Management**: Automatic table creation with proper indexes
- **Additional Properties**: JSON storage for flexible validation mode

### Validation Middleware
The `validator.go` file implements OpenAPI validation using kin-openapi:
- Dynamically loads different OpenAPI specifications based on mode
- Creates routers for request matching
- Validates incoming requests against the schema
- Provides user-friendly error messages

### Generated Code
- **generated/**: oapi-codegen output (types and server interfaces)
- **db/**: sqlc output (database models and queries)

### Database Schema
```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    age INTEGER NOT NULL CHECK(age >= 0),
    name TEXT,
    bio TEXT,
    is_active BOOLEAN NOT NULL DEFAULT 1,
    additional_data TEXT, -- JSON for extra properties
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## Development Commands

- `make install`: Install dependencies and tools (oapi-codegen, sqlc)
- `make generate`: Generate code from OpenAPI specs and SQL
- `make run`: Start default validation server
- `make run-flexible`: Start flexible validation server
- `make run-strict`: Start strict validation server
- `make test`: Test default mode
- `make test-flexible`: Test flexible mode
- `make test-strict`: Test strict mode
- `make clean`: Remove generated files and database