package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"openapi-validation-example/db"
	"openapi-validation-example/generated"
	"openapi-validation-example/pkg/jobs"

	openapi_types "github.com/oapi-codegen/runtime/types"
	_ "modernc.org/sqlite"
)

type DatabaseService struct {
	db       *sql.DB
	queries  *db.Queries
	jobQueue *jobs.JobQueueService
}

func NewDatabaseService(dbPath string) (*DatabaseService, error) {
	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	queries := db.New(database)

	if err := initSchema(database); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	jobQueue := jobs.NewJobQueueService(database)

	return &DatabaseService{
		db:      database,
		queries: queries,
		jobQueue: jobQueue,
	}, nil
}

func initSchema(database *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    age INTEGER NOT NULL CHECK(age >= 0),
    name TEXT,
    bio TEXT,
    is_active BOOLEAN NOT NULL DEFAULT 1,
    additional_data TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS job_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    retry_count INTEGER DEFAULT 0,
    error_message TEXT,
    scheduled_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_active ON users(is_active);
CREATE INDEX IF NOT EXISTS idx_job_queue_status ON job_queue(status);
CREATE INDEX IF NOT EXISTS idx_job_queue_type ON job_queue(job_type);
CREATE INDEX IF NOT EXISTS idx_job_queue_scheduled ON job_queue(scheduled_at);
CREATE INDEX IF NOT EXISTS idx_job_queue_priority ON job_queue(priority DESC, scheduled_at);`

	if _, err := database.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

func (ds *DatabaseService) CreateUser(userReq generated.UserRequest, additionalProps map[string]interface{}) (*generated.User, error) {
	var additionalData sql.NullString
	if len(additionalProps) > 0 {
		jsonData, err := json.Marshal(additionalProps)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal additional properties: %w", err)
		}
		additionalData = sql.NullString{String: string(jsonData), Valid: true}
	}

	var name sql.NullString
	if userReq.Name != nil {
		name = sql.NullString{String: *userReq.Name, Valid: true}
	}

	var bio sql.NullString
	if userReq.Bio != nil {
		bio = sql.NullString{String: *userReq.Bio, Valid: true}
	}

	isActive := true
	if userReq.IsActive != nil {
		isActive = *userReq.IsActive
	}

	dbUser, err := ds.queries.CreateUser(context.Background(), db.CreateUserParams{
		Email:          string(userReq.Email),
		Age:            int64(userReq.Age),
		Name:           name,
		Bio:            bio,
		IsActive:       isActive,
		AdditionalData: additionalData,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	user, err := ds.convertDBUserToGenerated(dbUser)
	if err != nil {
		return nil, err
	}

	// Enqueue background job for user created
	jobPayload := jobs.JobPayload{
		UserID:          &user.Id,
		UserData:        map[string]interface{}{
			"id":        user.Id,
			"email":     user.Email,
			"age":       user.Age,
			"name":      user.Name,
			"bio":       user.Bio,
			"is_active": user.IsActive,
		},
		AdditionalProps: additionalProps,
	}

	_, jobErr := ds.jobQueue.EnqueueJob(jobs.JobUserCreated, jobPayload, 1)
	if jobErr != nil {
		// Log error but don't fail the user creation
		fmt.Printf("Failed to enqueue job for user %d: %v\n", user.Id, jobErr)
	}

	return user, nil
}

func (ds *DatabaseService) GetUserByID(id int64) (*generated.User, error) {
	dbUser, err := ds.queries.GetUserByID(context.Background(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return ds.convertDBUserToGenerated(dbUser)
}

func (ds *DatabaseService) convertDBUserToGenerated(dbUser db.User) (*generated.User, error) {
	user := &generated.User{
		Id:    dbUser.ID,
		Email: openapi_types.Email(dbUser.Email),
		Age:   int(dbUser.Age),
	}

	if dbUser.Name.Valid {
		user.Name = &dbUser.Name.String
	}

	if dbUser.Bio.Valid {
		user.Bio = &dbUser.Bio.String
	}

	user.IsActive = &dbUser.IsActive

	return user, nil
}

func (ds *DatabaseService) Close() error {
	return ds.db.Close()
}

func (ds *DatabaseService) GetJobQueue() *jobs.JobQueueService {
	return ds.jobQueue
}