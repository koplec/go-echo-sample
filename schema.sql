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

-- Index for faster email lookups
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_active ON users(is_active);