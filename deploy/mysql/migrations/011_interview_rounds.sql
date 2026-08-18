USE hr_agent;

-- JD interview plan (structured rounds)
CREATE TABLE IF NOT EXISTS jd_interview_rounds (
    id                VARCHAR(64) PRIMARY KEY,
    jd_id             VARCHAR(64) NOT NULL,
    sort_order        INT NOT NULL DEFAULT 0,
    round_key         VARCHAR(64) NOT NULL DEFAULT '',
    name              VARCHAR(128) NOT NULL DEFAULT '',
    theme             TEXT NULL,
    duration_minutes  INT NOT NULL DEFAULT 60,
    advance           VARCHAR(32) NOT NULL DEFAULT 'hr_manual',
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uq_jd_round_order (jd_id, sort_order),
    INDEX idx_jd_rounds_jd (jd_id),
    CONSTRAINT fk_jd_rounds_jd FOREIGN KEY (jd_id) REFERENCES job_descriptions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS jd_round_interviewer_requirements (
    id                    VARCHAR(64) PRIMARY KEY,
    jd_round_id           VARCHAR(64) NOT NULL,
    role_kind             VARCHAR(32) NOT NULL DEFAULT 'tech',
    headcount             INT NOT NULL DEFAULT 1,
    pool_id               VARCHAR(64) NULL,
    match_jd_department   TINYINT(1) NOT NULL DEFAULT 0,
    specialties           JSON NULL,
    fixed_open_ids        JSON NULL,
    created_at            TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_req_round (jd_round_id),
    CONSTRAINT fk_req_round FOREIGN KEY (jd_round_id) REFERENCES jd_interview_rounds(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS application_interview_rounds (
    id                  VARCHAR(64) PRIMARY KEY,
    application_id      VARCHAR(64) NOT NULL,
    jd_round_id         VARCHAR(64) NULL,
    round_index         INT NOT NULL DEFAULT 0,
    status              VARCHAR(32) NOT NULL DEFAULT 'pending',
    assigned_open_ids   JSON NULL,
    assignment_detail   JSON NULL,
    confirmed_slot_id   VARCHAR(64) NULL,
    outcome             VARCHAR(32) NULL,
    feedback_json       JSON NULL,
    provider_event_id   VARCHAR(128) NULL,
    created_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uq_app_round (application_id, round_index),
    INDEX idx_air_app (application_id),
    CONSTRAINT fk_air_app FOREIGN KEY (application_id) REFERENCES applications(id) ON DELETE CASCADE
);

ALTER TABLE applications
  ADD COLUMN current_round_index INT NOT NULL DEFAULT 0 AFTER reschedule_count;

ALTER TABLE interview_slots
  ADD COLUMN application_round_id VARCHAR(64) NULL AFTER application_id,
  ADD COLUMN source VARCHAR(32) NOT NULL DEFAULT 'auto' AFTER is_proposed;

CREATE INDEX idx_slots_round ON interview_slots (application_round_id);
