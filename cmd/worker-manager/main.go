package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
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

	dbService, err := NewDatabaseService(dbPath)
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

func showJobStats(dbService *DatabaseService) {
	stats, err := dbService.jobQueue.GetJobStats()
	if err != nil {
		log.Fatalf("Failed to get job stats: %v", err)
	}

	fmt.Println("📊 Job Queue Statistics")
	fmt.Println("=" * 40)
	fmt.Printf("Pending:    %d jobs\n", stats.PendingCount)
	fmt.Printf("Processing: %d jobs\n", stats.ProcessingCount)
	fmt.Printf("Completed:  %d jobs\n", stats.CompletedCount)
	fmt.Printf("Failed:     %d jobs\n", stats.FailedCount)
	fmt.Printf("Total:      %d jobs\n",
		stats.PendingCount+stats.ProcessingCount+stats.CompletedCount+stats.FailedCount)
}

func listJobs(dbService *DatabaseService, status string) {
	jobs, err := dbService.jobQueue.ListJobs(status, 20)
	if err != nil {
		log.Fatalf("Failed to list jobs: %v", err)
	}

	fmt.Printf("📋 Jobs with status '%s' (last 20)\n", status)
	fmt.Println("=" * 60)

	if len(jobs) == 0 {
		fmt.Printf("No jobs found with status '%s'\n", status)
		return
	}

	for _, job := range jobs {
		fmt.Printf("ID: %d | Type: %s | Priority: %d | Retries: %d/%d\n",
			job.ID, job.JobType, job.Priority, job.RetryCount, job.MaxRetries)

		if job.ErrorMessage.Valid && job.ErrorMessage.String != "" {
			fmt.Printf("  Error: %s\n", job.ErrorMessage.String)
		}

		// Show payload preview
		var payload JobPayload
		if err := json.Unmarshal([]byte(job.Payload), &payload); err == nil {
			if payload.UserID != nil {
				fmt.Printf("  User ID: %d\n", *payload.UserID)
			}
			if payload.Message != "" {
				fmt.Printf("  Message: %s\n", payload.Message)
			}
		}

		fmt.Printf("  Created: %s\n", job.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Println()
	}
}

func enqueueTestJob(dbService *DatabaseService, jobTypeStr, message string, args []string) {
	priority := 0
	if len(args) > 0 {
		if p, err := strconv.Atoi(args[0]); err == nil {
			priority = p
		}
	}

	var jobType JobType
	switch jobTypeStr {
	case "user_created":
		jobType = JobUserCreated
	case "data_analysis":
		jobType = JobDataAnalysis
	case "email_notification":
		jobType = JobEmailNotification
	case "data_export":
		jobType = JobDataExport
	default:
		fmt.Printf("Invalid job type: %s\n", jobTypeStr)
		fmt.Println("Valid types: user_created, data_analysis, email_notification, data_export")
		os.Exit(1)
	}

	payload := JobPayload{
		Message: message,
	}

	// Add specific payload data based on job type
	switch jobType {
	case JobUserCreated:
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
	case JobEmailNotification:
		payload.Recipients = []string{"admin@example.com", "user@example.com"}
	}

	job, err := dbService.jobQueue.EnqueueJob(jobType, payload, priority)
	if err != nil {
		log.Fatalf("Failed to enqueue job: %v", err)
	}

	fmt.Printf("✅ Job enqueued successfully!\n")
	fmt.Printf("ID: %d | Type: %s | Priority: %d\n", job.ID, job.JobType, job.Priority)
	fmt.Printf("Scheduled: %s\n", job.ScheduledAt.Format("2006-01-02 15:04:05"))
}

func clearJobs(dbService *DatabaseService, status string) {
	jobs, err := dbService.jobQueue.ListJobs(status, 1000)
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