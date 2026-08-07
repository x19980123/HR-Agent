USE hr_agent;

CREATE TABLE IF NOT EXISTS staff_members (
    open_id         VARCHAR(128) PRIMARY KEY,
    name            VARCHAR(128) DEFAULT '',
    email           VARCHAR(255) DEFAULT '',
    is_hr           TINYINT(1) NOT NULL DEFAULT 0,
    is_interviewer  TINYINT(1) NOT NULL DEFAULT 0,
    enabled         TINYINT(1) NOT NULL DEFAULT 1,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_staff_hr (is_hr, enabled),
    INDEX idx_staff_interviewer (is_interviewer, enabled)
);
