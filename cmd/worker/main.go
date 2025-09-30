package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"openapi-validation-example/db"
	"openapi-validation-example/pkg/database"
	"openapi-validation-example/pkg/jobs"
)

type Worker struct {
	id           int
	jobQueue     *jobs.JobQueueService
	stopCh       chan struct{}
	wg           *sync.WaitGroup
	processingWg *sync.WaitGroup
}

type JobProcessor interface {
	Process(job *db.JobQueue, payload jobs.JobPayload) error
	JobType() jobs.JobType
}

// UserCreatedProcessor handles user creation jobs
type UserCreatedProcessor struct{}

func (p *UserCreatedProcessor) JobType() jobs.JobType {
	return jobs.JobUserCreated
}

func (p *UserCreatedProcessor) Process(job *db.JobQueue, payload jobs.JobPayload) error {
	log.Printf("Processing user created job %d for user %d", job.ID, *payload.UserID)

	// Simulate various processing tasks
	time.Sleep(time.Millisecond * 500) // Simulate work

	// Example processing tasks:
	fmt.Printf("📧 Sending welcome email to user %d (%s)\n", *payload.UserID, payload.UserData["email"])

	if len(payload.AdditionalProps) > 0 {
		fmt.Printf("🔍 Analyzing additional user properties: %v\n", payload.AdditionalProps)

		// Example: Log interesting additional properties
		for key, value := range payload.AdditionalProps {
			switch key {
			case "hobby":
				fmt.Printf("   - User's hobby: %v\n", value)
			case "location":
				fmt.Printf("   - User's location: %v\n", value)
			case "score":
				fmt.Printf("   - User's score: %v\n", value)
			default:
				fmt.Printf("   - Custom field %s: %v\n", key, value)
			}
		}
	}

	// Simulate analytics
	fmt.Printf("📊 Recording user signup metrics for user %d\n", *payload.UserID)

	// Simulate profile setup
	fmt.Printf("⚙️  Setting up user profile for user %d\n", *payload.UserID)

	return nil
}

// DataAnalysisProcessor handles data analysis jobs
type DataAnalysisProcessor struct{}

func (p *DataAnalysisProcessor) JobType() jobs.JobType {
	return jobs.JobDataAnalysis
}

func (p *DataAnalysisProcessor) Process(job *db.JobQueue, payload jobs.JobPayload) error {
	log.Printf("Processing data analysis job %d", job.ID)

	time.Sleep(time.Second * 2) // Simulate longer analysis

	fmt.Printf("📈 Performing data analysis: %s\n", payload.Message)
	fmt.Printf("📊 Analysis completed with insights\n")

	return nil
}

// EmailNotificationProcessor handles email notification jobs
type EmailNotificationProcessor struct{}

func (p *EmailNotificationProcessor) JobType() jobs.JobType {
	return jobs.JobEmailNotification
}

func (p *EmailNotificationProcessor) Process(job *db.JobQueue, payload jobs.JobPayload) error {
	log.Printf("Processing email notification job %d", job.ID)

	time.Sleep(time.Millisecond * 300)

	for _, recipient := range payload.Recipients {
		fmt.Printf("📬 Sending email to %s: %s\n", recipient, payload.Message)
	}

	return nil
}

func NewWorker(id int, jobQueue *jobs.JobQueueService, wg *sync.WaitGroup) *Worker {
	return &Worker{
		id:           id,
		jobQueue:     jobQueue,
		stopCh:       make(chan struct{}),
		wg:           wg,
		processingWg: &sync.WaitGroup{},
	}
}

func (w *Worker) Start() {
	defer w.wg.Done()

	processors := map[jobs.JobType]JobProcessor{
		jobs.JobUserCreated:       &UserCreatedProcessor{},
		jobs.JobDataAnalysis:      &DataAnalysisProcessor{},
		jobs.JobEmailNotification: &EmailNotificationProcessor{},
	}

	log.Printf("Worker %d started", w.id)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			log.Printf("Worker %d received stop signal", w.id)
			w.processingWg.Wait() // Wait for current jobs to complete
			log.Printf("Worker %d stopped", w.id)
			return
		case <-ticker.C:
			w.processNextJob(processors)
		}
	}
}

func (w *Worker) processNextJob(processors map[jobs.JobType]JobProcessor) {
	job, err := w.jobQueue.GetNextJob()
	if err != nil {
		log.Printf("Worker %d: Error getting next job: %v", w.id, err)
		return
	}

	if job == nil {
		// No jobs available
		return
	}

	w.processingWg.Add(1)
	go func() {
		defer w.processingWg.Done()

		log.Printf("Worker %d: Processing job %d (type: %s)", w.id, job.ID, job.JobType)

		// Parse payload
		var payload jobs.JobPayload
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			log.Printf("Worker %d: Error parsing job payload: %v", w.id, err)
			w.jobQueue.FailJob(job.ID, fmt.Sprintf("Failed to parse payload: %v", err), false)
			return
		}

		// Find processor
		processor, exists := processors[jobs.JobType(job.JobType)]
		if !exists {
			log.Printf("Worker %d: No processor found for job type: %s", w.id, job.JobType)
			w.jobQueue.FailJob(job.ID, fmt.Sprintf("No processor for job type: %s", job.JobType), false)
			return
		}

		// Process the job
		if err := processor.Process(job, payload); err != nil {
			log.Printf("Worker %d: Job %d failed: %v", w.id, job.ID, err)

			// Retry logic
			var retryCount, maxRetries int64
			if job.RetryCount.Valid {
				retryCount = job.RetryCount.Int64
			}
			if job.MaxRetries.Valid {
				maxRetries = job.MaxRetries.Int64
			}
			shouldRetry := retryCount < maxRetries
			w.jobQueue.FailJob(job.ID, err.Error(), shouldRetry)
		} else {
			log.Printf("Worker %d: Job %d completed successfully", w.id, job.ID)
			w.jobQueue.CompleteJob(job.ID)
		}
	}()
}

func (w *Worker) Stop() {
	close(w.stopCh)
}

func main() {
	dbPath := "workers.db"
	if len(os.Args) > 1 && os.Args[1] != "" {
		dbPath = os.Args[1]
	}

	log.Printf("Starting worker manager with database: %s", dbPath)

	// Initialize database
	dbService, err := database.NewDatabaseService(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer dbService.Close()

	// Number of concurrent workers
	numWorkers := 3
	if workerCount := os.Getenv("WORKER_COUNT"); workerCount != "" {
		fmt.Sscanf(workerCount, "%d", &numWorkers)
	}

	log.Printf("Starting %d workers...", numWorkers)

	var wg sync.WaitGroup
	workers := make([]*Worker, numWorkers)

	// Start workers
	for i := 0; i < numWorkers; i++ {
		workers[i] = NewWorker(i+1, dbService.GetJobQueue(), &wg)
		wg.Add(1)
		go workers[i].Start()
	}

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("Worker manager started. Press Ctrl+C to stop.")

	// Print job stats periodically
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-sigCh:
				return
			case <-ticker.C:
				stats, err := dbService.GetJobQueue().GetJobStats()
				if err == nil {
					log.Printf("Job Stats - Pending: %d, Processing: %d, Completed: %d, Failed: %d",
						stats.PendingCount, stats.ProcessingCount, stats.CompletedCount, stats.FailedCount)
				}
			}
		}
	}()

	// Wait for shutdown signal
	<-sigCh
	log.Println("Received shutdown signal. Stopping workers...")

	// Stop all workers
	for _, worker := range workers {
		worker.Stop()
	}

	// Wait for all workers to finish
	wg.Wait()
	log.Println("All workers stopped. Goodbye!")
}