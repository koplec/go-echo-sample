CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    age INTEGER NOT NULL CHECK(age >= 0),
    name TEXT,
    bio TEXT,
    is_active BOOLEAN NOT NULL DEFAULT 1,
    additional_data TEXT, -- JSON string for additional properties
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Job queue table for background processing
CREATE TABLE job_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_type TEXT NOT NULL, -- 'user_created', 'data_analysis', 'email_notification', etc.
    payload TEXT NOT NULL,  -- JSON data to process
    status TEXT NOT NULL DEFAULT 'pending', -- 'pending', 'processing', 'completed', 'failed'
    priority INTEGER DEFAULT 0, -- Higher number = higher priority
    max_retries INTEGER DEFAULT 3,
    retry_count INTEGER DEFAULT 0,
    error_message TEXT,
    scheduled_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Index for faster email lookups
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_active ON users(is_active);

-- Indexes for job queue
CREATE INDEX idx_job_queue_status ON job_queue(status);
CREATE INDEX idx_job_queue_type ON job_queue(job_type);
CREATE INDEX idx_job_queue_scheduled ON job_queue(scheduled_at);
CREATE INDEX idx_job_queue_priority ON job_queue(priority DESC, scheduled_at);