package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"openapi-validation-example/pkg/database"
	"openapi-validation-example/pkg/jobs"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	dbPath := "users.db"
	if len(os.Args) > 2 {
		dbPath = os.Args[2]
	}

	dbService, err := database.NewDatabaseService(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer dbService.Close()

	switch command {
	case "stats":
		showJobStats(dbService)
	case "list":
		status := "pending"
		if len(os.Args) > 3 {
			status = os.Args[3]
		}
		listJobs(dbService, status)
	case "enqueue":
		if len(os.Args) < 5 {
			fmt.Println("Usage: worker-manager enqueue <job_type> <message> [priority]")
			os.Exit(1)
		}
		enqueueTestJob(dbService, os.Args[3], os.Args[4], os.Args[5:])
	case "clear":
		status := "completed"
		if len(os.Args) > 3 {
			status = os.Args[3]
		}
		clearJobs(dbService, status)
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Worker Manager - Job Queue Management Tool")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  worker-manager <command> [database_path] [args...]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  stats                     Show job queue statistics")
	fmt.Println("  list [status]            List jobs by status (default: pending)")
	fmt.Println("  enqueue <type> <msg> [p] Enqueue a test job")
	fmt.Println("  clear [status]           Clear jobs by status (default: completed)")
	fmt.Println()
	fmt.Println("Job Types:")
	fmt.Println("  user_created, data_analysis, email_notification, data_export")
	fmt.Println()
	fmt.Println("Job Statuses:")
	fmt.Println("  pending, processing, completed, failed")
}

func showJobStats(dbService *database.DatabaseService) {
	stats, err := dbService.GetJobQueue().GetJobStats()
	if err != nil {
		log.Fatalf("Failed to get job stats: %v", err)
	}

	fmt.Println("📊 Job Queue Statistics")
	fmt.Println(strings.Repeat("=", 40))
	fmt.Printf("Pending:    %d jobs\n", stats.PendingCount)
	fmt.Printf("Processing: %d jobs\n", stats.ProcessingCount)
	fmt.Printf("Completed:  %d jobs\n", stats.CompletedCount)
	fmt.Printf("Failed:     %d jobs\n", stats.FailedCount)
	fmt.Printf("Total:      %d jobs\n",
		stats.PendingCount+stats.ProcessingCount+stats.CompletedCount+stats.FailedCount)
}

func listJobs(dbService *database.DatabaseService, status string) {
	jobs, err := dbService.GetJobQueue().ListJobs(status, 20)
	if err != nil {
		log.Fatalf("Failed to list jobs: %v", err)
	}

	fmt.Printf("📋 Jobs with status '%s' (last 20)\n", status)
	fmt.Println(strings.Repeat("=", 60))

	if len(jobs) == 0 {
		fmt.Printf("No jobs found with status '%s'\n", status)
		return
	}

	for _, job := range jobs {
		var priority, retryCount, maxRetries int64
		if job.Priority.Valid {
			priority = job.Priority.Int64
		}
		if job.RetryCount.Valid {
			retryCount = job.RetryCount.Int64
		}
		if job.MaxRetries.Valid {
			maxRetries = job.MaxRetries.Int64
		}

		fmt.Printf("ID: %d | Type: %s | Priority: %d | Retries: %d/%d\n",
			job.ID, job.JobType, priority, retryCount, maxRetries)

		if job.ErrorMessage.Valid && job.ErrorMessage.String != "" {
			fmt.Printf("  Error: %s\n", job.ErrorMessage.String)
		}

		// Show payload preview
		type JobPayloadPreview struct {
			UserID  *int64 `json:"user_id,omitempty"`
			Message string `json:"message,omitempty"`
		}
		var payload JobPayloadPreview
		if err := json.Unmarshal([]byte(job.Payload), &payload); err == nil {
			if payload.UserID != nil {
				fmt.Printf("  User ID: %d\n", *payload.UserID)
			}
			if payload.Message != "" {
				fmt.Printf("  Message: %s\n", payload.Message)
			}
		}

		if job.CreatedAt.Valid {
			fmt.Printf("  Created: %s\n", job.CreatedAt.Time.Format("2006-01-02 15:04:05"))
		}
		fmt.Println()
	}
}

func enqueueTestJob(dbService *database.DatabaseService, jobTypeStr, message string, args []string) {
	priority := 0
	if len(args) > 0 {
		if p, err := strconv.Atoi(args[0]); err == nil {
			priority = p
		}
	}

	var jobType jobs.JobType
	switch jobTypeStr {
	case "user_created":
		jobType = jobs.JobUserCreated
	case "data_analysis":
		jobType = jobs.JobDataAnalysis
	case "email_notification":
		jobType = jobs.JobEmailNotification
	case "data_export":
		jobType = jobs.JobDataExport
	default:
		fmt.Printf("Invalid job type: %s\n", jobTypeStr)
		fmt.Println("Valid types: user_created, data_analysis, email_notification, data_export")
		os.Exit(1)
	}

	payload := jobs.JobPayload{
		Message: message,
	}

	// Add specific payload data based on job type
	switch jobType {
	case jobs.JobUserCreated:
		userID := int64(999)
		payload.UserID = &userID
		payload.UserData = map[string]interface{}{
			"id":    999,
			"email": "test@example.com",
			"age":   25,
		}
		payload.AdditionalProps = map[string]interface{}{
			"test_data": true,
			"source":    "worker-manager",
		}
	case jobs.JobEmailNotification:
		payload.Recipients = []string{"admin@example.com", "user@example.com"}
	}

	job, err := dbService.GetJobQueue().EnqueueJob(jobType, payload, priority)
	if err != nil {
		log.Fatalf("Failed to enqueue job: %v", err)
	}

	fmt.Printf("✅ Job enqueued successfully!\n")
	var jobPriority int64
	if job.Priority.Valid {
		jobPriority = job.Priority.Int64
	}
	fmt.Printf("ID: %d | Type: %s | Priority: %d\n", job.ID, job.JobType, jobPriority)
	if job.ScheduledAt.Valid {
		fmt.Printf("Scheduled: %s\n", job.ScheduledAt.Time.Format("2006-01-02 15:04:05"))
	}
}

func clearJobs(dbService *database.DatabaseService, status string) {
	jobs, err := dbService.GetJobQueue().ListJobs(status, 1000)
	if err != nil {
		log.Fatalf("Failed to list jobs: %v", err)
	}

	if len(jobs) == 0 {
		fmt.Printf("No jobs found with status '%s'\n", status)
		return
	}

	fmt.Printf("Found %d jobs with status '%s'\n", len(jobs), status)
	fmt.Print("Are you sure you want to delete them? (y/N): ")

	var response string
	fmt.Scanln(&response)

	if response != "y" && response != "Y" {
		fmt.Println("Cancelled.")
		return
	}

	// Note: This would require implementing a DeleteJobs method in JobQueueService
	fmt.Printf("⚠️  Clear functionality not yet implemented.\n")
	fmt.Printf("Jobs with status '%s' found: %d\n", status, len(jobs))
}