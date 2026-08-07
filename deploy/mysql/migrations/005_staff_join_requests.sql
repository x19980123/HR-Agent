USE hr_agent;

CREATE TABLE IF NOT EXISTS staff_join_requests (
    id              VARCHAR(64) PRIMARY KEY,
    open_id         VARCHAR(128) NOT NULL,
    name            VARCHAR(128) DEFAULT '',
    email           VARCHAR(255) DEFAULT '',
    avatar          VARCHAR(512) DEFAULT '',
    status          VARCHAR(32) NOT NULL DEFAULT 'pending',
    note            VARCHAR(512) DEFAULT '',
    decided_by      VARCHAR(128) DEFAULT '',
    decided_at      TIMESTAMP NULL,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_join_open (open_id),
    INDEX idx_join_status (status, created_at)
);
