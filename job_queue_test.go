package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestJobQueue(t *testing.T) (*JobQueueService, *DatabaseService) {
	testDBPath := "test_job_queue.db"
	os.Remove(testDBPath) // Clean up any existing test DB

	db, err := NewDatabaseService(testDBPath)
	require.NoError(t, err)

	jobQueue := NewJobQueueService(db)

	t.Cleanup(func() {
		db.queries.db.Close()
		os.Remove(testDBPath)
	})

	return jobQueue, db
}

func TestJobQueueService_EnqueueJob(t *testing.T) {
	jobQueue, _ := setupTestJobQueue(t)

	tests := []struct {
		name     string
		jobType  JobType
		payload  map[string]interface{}
		priority int
	}{
		{
			name:    "User created job",
			jobType: JobTypeUserCreated,
			payload: map[string]interface{}{
				"user_id": 1,
				"email":   "test@example.com",
				"extra":   "additional_data",
			},
			priority: 1,
		},
		{
			name:     "Data analysis job",
			jobType:  JobTypeDataAnalysis,
			payload:  map[string]interface{}{"data": "analysis_payload"},
			priority: 2,
		},
		{
			name:     "Email notification job",
			jobType:  JobTypeEmailNotification,
			payload:  map[string]interface{}{"recipient": "user@example.com"},
			priority: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, err := jobQueue.EnqueueJob(tt.jobType, tt.payload, tt.priority)

			require.NoError(t, err)
			assert.NotNil(t, job)
			assert.NotZero(t, job.ID)
			assert.Equal(t, string(tt.jobType), job.JobType)
			assert.Equal(t, "pending", job.Status)
		})
	}
}

func TestJobQueueService_GetPendingJobs(t *testing.T) {
	jobQueue, _ := setupTestJobQueue(t)

	// Enqueue test jobs
	jobs := []struct {
		jobType  JobType
		payload  map[string]interface{}
		priority int
	}{
		{JobTypeUserCreated, map[string]interface{}{"user_id": 1}, 1},
		{JobTypeDataAnalysis, map[string]interface{}{"data": "test"}, 2},
		{JobTypeEmailNotification, map[string]interface{}{"email": "test@example.com"}, 0},
	}

	for _, job := range jobs {
		_, err := jobQueue.EnqueueJob(job.jobType, job.payload, job.priority)
		require.NoError(t, err)
	}

	// Get pending jobs
	pendingJobs, err := jobQueue.GetPendingJobs(10)
	require.NoError(t, err)

	assert.Len(t, pendingJobs, 3)

	// Verify jobs are ordered by priority (higher priority first)
	assert.Equal(t, "data_analysis", pendingJobs[0].JobType) // priority 2
	assert.Equal(t, "user_created", pendingJobs[1].JobType)  // priority 1
	assert.Equal(t, "email_notification", pendingJobs[2].JobType) // priority 0
}

func TestJobQueueService_StartJob(t *testing.T) {
	jobQueue, _ := setupTestJobQueue(t)

	// Enqueue a test job
	job, err := jobQueue.EnqueueJob(JobTypeUserCreated, map[string]interface{}{"test": "data"}, 1)
	require.NoError(t, err)

	// Start the job
	err = jobQueue.StartJob(job.ID)
	require.NoError(t, err)

	// Verify job status changed to processing
	updatedJob, err := jobQueue.GetJobByID(job.ID)
	require.NoError(t, err)
	assert.Equal(t, "processing", updatedJob.Status)
	assert.True(t, updatedJob.StartedAt.Valid)
}

func TestJobQueueService_CompleteJob(t *testing.T) {
	jobQueue, _ := setupTestJobQueue(t)

	// Enqueue and start a job
	job, err := jobQueue.EnqueueJob(JobTypeUserCreated, map[string]interface{}{"test": "data"}, 1)
	require.NoError(t, err)

	err = jobQueue.StartJob(job.ID)
	require.NoError(t, err)

	// Complete the job
	err = jobQueue.CompleteJob(job.ID)
	require.NoError(t, err)

	// Verify job status changed to completed
	completedJob, err := jobQueue.GetJobByID(job.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", completedJob.Status)
	assert.True(t, completedJob.CompletedAt.Valid)
}

func TestJobQueueService_FailJob(t *testing.T) {
	jobQueue, _ := setupTestJobQueue(t)

	// Enqueue and start a job
	job, err := jobQueue.EnqueueJob(JobTypeUserCreated, map[string]interface{}{"test": "data"}, 1)
	require.NoError(t, err)

	err = jobQueue.StartJob(job.ID)
	require.NoError(t, err)

	// Fail the job
	errorMessage := "Test error message"
	err = jobQueue.FailJob(job.ID, errorMessage)
	require.NoError(t, err)

	// Verify job status and error message
	failedJob, err := jobQueue.GetJobByID(job.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", failedJob.Status)
	assert.True(t, failedJob.ErrorMessage.Valid)
	assert.Equal(t, errorMessage, failedJob.ErrorMessage.String)
	assert.Equal(t, int64(1), failedJob.RetryCount.Int64)
}

func TestJobQueueService_GetJobStats(t *testing.T) {
	jobQueue, _ := setupTestJobQueue(t)

	// Create jobs in different states
	// Pending job
	pendingJob, err := jobQueue.EnqueueJob(JobTypeUserCreated, map[string]interface{}{"test": "pending"}, 1)
	require.NoError(t, err)

	// Processing job
	processingJob, err := jobQueue.EnqueueJob(JobTypeDataAnalysis, map[string]interface{}{"test": "processing"}, 1)
	require.NoError(t, err)
	err = jobQueue.StartJob(processingJob.ID)
	require.NoError(t, err)

	// Completed job
	completedJob, err := jobQueue.EnqueueJob(JobTypeEmailNotification, map[string]interface{}{"test": "completed"}, 1)
	require.NoError(t, err)
	err = jobQueue.StartJob(completedJob.ID)
	require.NoError(t, err)
	err = jobQueue.CompleteJob(completedJob.ID)
	require.NoError(t, err)

	// Failed job
	failedJob, err := jobQueue.EnqueueJob(JobTypeDataExport, map[string]interface{}{"test": "failed"}, 1)
	require.NoError(t, err)
	err = jobQueue.StartJob(failedJob.ID)
	require.NoError(t, err)
	err = jobQueue.FailJob(failedJob.ID, "Test failure")
	require.NoError(t, err)

	// Get stats
	stats, err := jobQueue.GetJobStats()
	require.NoError(t, err)

	assert.Equal(t, 1, stats.Pending)
	assert.Equal(t, 1, stats.Processing)
	assert.Equal(t, 1, stats.Completed)
	assert.Equal(t, 1, stats.Failed)
	assert.Equal(t, 4, stats.Total)
}

func TestJobProcessor_ProcessUserCreatedJob(t *testing.T) {
	processor := NewJobProcessor(nil, nil)

	payload := map[string]interface{}{
		"user_id": float64(1), // JSON numbers are float64
		"email":   "test@example.com",
		"name":    "Test User",
	}

	// This should not error (just log the processing)
	err := processor.ProcessUserCreatedJob(payload)
	assert.NoError(t, err)
}

func TestJobProcessor_ProcessDataAnalysisJob(t *testing.T) {
	processor := NewJobProcessor(nil, nil)

	payload := map[string]interface{}{
		"data_type": "user_behavior",
		"data":      "analysis_data",
	}

	err := processor.ProcessDataAnalysisJob(payload)
	assert.NoError(t, err)
}

func TestJobProcessor_ProcessEmailNotificationJob(t *testing.T) {
	processor := NewJobProcessor(nil, nil)

	payload := map[string]interface{}{
		"recipient": "user@example.com",
		"subject":   "Welcome!",
		"template":  "welcome",
	}

	err := processor.ProcessEmailNotificationJob(payload)
	assert.NoError(t, err)
}

func TestJobProcessor_ProcessDataExportJob(t *testing.T) {
	processor := NewJobProcessor(nil, nil)

	payload := map[string]interface{}{
		"format":      "json",
		"destination": "s3://bucket/export.json",
		"data":        []interface{}{"item1", "item2"},
	}

	err := processor.ProcessDataExportJob(payload)
	assert.NoError(t, err)
}

func TestJobProcessor_ProcessJob_InvalidJobType(t *testing.T) {
	jobQueue, _ := setupTestJobQueue(t)
	processor := NewJobProcessor(jobQueue, nil)

	// Create a job with invalid type manually in database
	job, err := jobQueue.EnqueueJob(JobType("invalid_type"), map[string]interface{}{"test": "data"}, 1)
	require.NoError(t, err)

	err = processor.ProcessJob(context.Background(), job)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown job type")
}

// Integration test that simulates real workflow
func TestJobWorkflow_Integration(t *testing.T) {
	jobQueue, db := setupTestJobQueue(t)

	// Create a user (this would normally trigger job enqueuing in real app)
	userReq := generated.UserRequest{
		Email: "workflow@example.com",
		Age:   30,
	}

	user, err := db.CreateUser(userReq, map[string]interface{}{
		"source": "integration_test",
		"metadata": "test_data",
	})
	require.NoError(t, err)

	// Manually enqueue job (simulating what CreateUser would do)
	jobPayload := map[string]interface{}{
		"user_id": user.Id,
		"email":   string(user.Email),
		"age":     user.Age,
		"source":  "integration_test",
	}

	job, err := jobQueue.EnqueueJob(JobTypeUserCreated, jobPayload, 1)
	require.NoError(t, err)

	// Process the job
	processor := NewJobProcessor(jobQueue, db)

	// Start job
	err = jobQueue.StartJob(job.ID)
	require.NoError(t, err)

	// Process job
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = processor.ProcessJob(ctx, job)
	require.NoError(t, err)

	// Verify job completed
	completedJob, err := jobQueue.GetJobByID(job.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", completedJob.Status)
	assert.True(t, completedJob.CompletedAt.Valid)
}