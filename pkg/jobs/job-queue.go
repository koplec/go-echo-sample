package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"openapi-validation-example/db"

	_ "modernc.org/sqlite"
)

type JobType string

const (
	JobUserCreated      JobType = "user_created"
	JobDataAnalysis     JobType = "data_analysis"
	JobEmailNotification JobType = "email_notification"
	JobDataExport       JobType = "data_export"
)

type JobPayload struct {
	UserID           *int64                 `json:"user_id,omitempty"`
	UserData         map[string]interface{} `json:"user_data,omitempty"`
	AdditionalProps  map[string]interface{} `json:"additional_props,omitempty"`
	Message          string                 `json:"message,omitempty"`
	Recipients       []string               `json:"recipients,omitempty"`
	ValidationMode   string                 `json:"validation_mode,omitempty"`
}

type JobQueueService struct {
	db      *sql.DB
	queries *db.Queries
}

func NewJobQueueService(database *sql.DB) *JobQueueService {
	return &JobQueueService{
		db:      database,
		queries: db.New(database),
	}
}

func (jq *JobQueueService) EnqueueJob(jobType JobType, payload JobPayload, priority int) (*db.JobQueue, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	job, err := jq.queries.CreateJob(context.Background(), db.CreateJobParams{
		JobType:     string(jobType),
		Payload:     string(payloadJSON),
		Priority:    sql.NullInt64{Int64: int64(priority), Valid: true},
		MaxRetries:  sql.NullInt64{Int64: 3, Valid: true},
		ScheduledAt: sql.NullTime{Time: time.Now(), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	return &job, nil
}

func (jq *JobQueueService) GetNextJob() (*db.JobQueue, error) {
	job, err := jq.queries.GetNextPendingJob(context.Background())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No jobs available
		}
		return nil, fmt.Errorf("failed to get next job: %w", err)
	}

	// Mark job as processing
	_, err = jq.queries.UpdateJobStatus(context.Background(), db.UpdateJobStatusParams{
		ID:          job.ID,
		Status:      "processing",
		StartedAt:   sql.NullTime{Time: time.Now(), Valid: true},
		CompletedAt: sql.NullTime{Valid: false},
		ErrorMessage: sql.NullString{Valid: false},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update job status: %w", err)
	}

	job.Status = "processing"
	return &job, nil
}

func (jq *JobQueueService) CompleteJob(jobID int64) error {
	_, err := jq.queries.UpdateJobStatus(context.Background(), db.UpdateJobStatusParams{
		ID:          jobID,
		Status:      "completed",
		StartedAt:   sql.NullTime{Valid: false}, // Keep existing value
		CompletedAt: sql.NullTime{Time: time.Now(), Valid: true},
		ErrorMessage: sql.NullString{Valid: false},
	})
	return err
}

func (jq *JobQueueService) FailJob(jobID int64, errorMessage string, retry bool) error {
	if retry {
		_, err := jq.queries.IncrementJobRetry(context.Background(), db.IncrementJobRetryParams{
			ID:           jobID,
			ErrorMessage: sql.NullString{String: errorMessage, Valid: true},
		})
		return err
	} else {
		_, err := jq.queries.UpdateJobStatus(context.Background(), db.UpdateJobStatusParams{
			ID:           jobID,
			Status:       "failed",
			StartedAt:    sql.NullTime{Valid: false},
			CompletedAt:  sql.NullTime{Time: time.Now(), Valid: true},
			ErrorMessage: sql.NullString{String: errorMessage, Valid: true},
		})
		return err
	}
}

func (jq *JobQueueService) GetJobStats() (*db.GetJobStatsRow, error) {
	stats, err := jq.queries.GetJobStats(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get job stats: %w", err)
	}
	return &stats, nil
}

func (jq *JobQueueService) ListJobs(status string, limit int) ([]db.JobQueue, error) {
	jobs, err := jq.queries.ListJobs(context.Background(), db.ListJobsParams{
		Status: status,
		Limit:  int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}
	return jobs, nil
}