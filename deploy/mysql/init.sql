CREATE DATABASE IF NOT EXISTS hr_agent CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE hr_agent;

CREATE TABLE IF NOT EXISTS job_descriptions (
    id            VARCHAR(64) PRIMARY KEY,
    title         VARCHAR(255) NOT NULL,
    department    VARCHAR(128) DEFAULT '',
    salary        VARCHAR(128) DEFAULT '',
    location      VARCHAR(255) DEFAULT '',
    description   TEXT NULL,
    requirements  JSON NOT NULL,
    weight_json   JSON NOT NULL,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS applications (
    id              VARCHAR(64) PRIMARY KEY,
    jd_id           VARCHAR(64) NOT NULL,
    candidate_email VARCHAR(255) NOT NULL,
    candidate_name  VARCHAR(128) DEFAULT '',
    resume_path     VARCHAR(512) NOT NULL,
    status          VARCHAR(64) NOT NULL,
    profile_json    JSON NULL,
    screen_json     JSON NULL,
    questions_json  JSON NULL,
    reply_intent    VARCHAR(32) NULL,
    reschedule_count INT NOT NULL DEFAULT 0,
    thread_id       VARCHAR(128) NOT NULL,
    last_message_id VARCHAR(255) NULL,
    langsmith_run_id VARCHAR(128) NULL,
    error_message   TEXT NULL,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_status (status),
    INDEX idx_thread (thread_id),
    CONSTRAINT fk_app_jd FOREIGN KEY (jd_id) REFERENCES job_descriptions(id)
);

CREATE TABLE IF NOT EXISTS interview_slots (
    id              VARCHAR(64) PRIMARY KEY,
    application_id  VARCHAR(64) NOT NULL,
    starts_at       DATETIME NOT NULL,
    ends_at         DATETIME NOT NULL,
    location        VARCHAR(255) DEFAULT '线上会议',
    status          VARCHAR(32) NOT NULL,
    provider_event_id VARCHAR(128) NULL,
    meeting_url     VARCHAR(512) NULL,
    is_proposed     TINYINT(1) NOT NULL DEFAULT 0,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_app_slots (application_id),
    CONSTRAINT fk_slot_app FOREIGN KEY (application_id) REFERENCES applications(id)
);

CREATE TABLE IF NOT EXISTS email_outbox (
    id              VARCHAR(64) PRIMARY KEY,
    application_id  VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL UNIQUE,
    to_addr         VARCHAR(255) NOT NULL,
    subject         VARCHAR(512) NOT NULL,
    body            TEXT NOT NULL,
    status          VARCHAR(32) NOT NULL,
    attempts        INT NOT NULL DEFAULT 0,
    last_error      TEXT NULL,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    sent_at         TIMESTAMP NULL,
    INDEX idx_outbox_status (status)
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    idem_key        VARCHAR(128) PRIMARY KEY,
    action          VARCHAR(64) NOT NULL,
    application_id  VARCHAR(64) NOT NULL,
    result_json     JSON NULL,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS audit_events (
    id              BIGINT AUTO_INCREMENT PRIMARY KEY,
    application_id  VARCHAR(64) NOT NULL,
    action          VARCHAR(64) NOT NULL,
    actor           VARCHAR(64) NOT NULL DEFAULT 'system',
    before_status   VARCHAR(64) NULL,
    after_status    VARCHAR(64) NULL,
    detail_json     JSON NULL,
    idempotency_key VARCHAR(128) NULL,
    langsmith_run_id VARCHAR(128) NULL,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_audit_app (application_id)
);

-- 飞书登录加入申请（未入职成员先申请，管理员审批）
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

-- HR / 面试官成员（系统登录看 is_hr；日历共享看 is_hr|is_interviewer）
CREATE TABLE IF NOT EXISTS staff_members (
    open_id         VARCHAR(128) PRIMARY KEY,
    name            VARCHAR(128) DEFAULT '',
    email           VARCHAR(255) DEFAULT '',
    is_hr           TINYINT(1) NOT NULL DEFAULT 0,
    is_interviewer  TINYINT(1) NOT NULL DEFAULT 0,
    is_admin        TINYINT(1) NOT NULL DEFAULT 0,
    enabled         TINYINT(1) NOT NULL DEFAULT 1,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_staff_hr (is_hr, enabled),
    INDEX idx_staff_interviewer (is_interviewer, enabled),
    INDEX idx_staff_admin (is_admin, enabled)
);

-- 题库知识（MySQL 为权威源；向量块同步到 Chroma）
CREATE TABLE IF NOT EXISTS question_bank (
    id              VARCHAR(64) PRIMARY KEY,
    category        VARCHAR(64) NOT NULL DEFAULT 'fundamentals',
    title           VARCHAR(255) DEFAULT '',
    content         TEXT NOT NULL,
    tags_json       JSON NULL,
    difficulty      VARCHAR(32) DEFAULT 'medium',
    jd_id           VARCHAR(64) NULL,
    enabled         TINYINT(1) NOT NULL DEFAULT 1,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_bank_cat (category),
    INDEX idx_bank_enabled (enabled)
);

INSERT INTO job_descriptions (id, title, department, requirements, weight_json) VALUES
('jd-backend-001', '后端工程师', '技术部',
 JSON_OBJECT(
   'education', '本科及以上',
   'major', JSON_ARRAY('计算机', '软件工程', '相关专业'),
   'years', 3,
   'skills', JSON_ARRAY('Go', 'Python', 'MySQL', 'Redis', '微服务')
 ),
 JSON_OBJECT('education', 15, 'major', 10, 'years', 20, 'skills', 35, 'projects', 15, 'papers', 5)
) ON DUPLICATE KEY UPDATE title=VALUES(title);
