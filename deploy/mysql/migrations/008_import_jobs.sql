USE hr_agent;

CREATE TABLE IF NOT EXISTS import_jobs (
  id CHAR(36) PRIMARY KEY,
  created_by VARCHAR(64) NOT NULL DEFAULT '',
  jd_id VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  total INT NOT NULL DEFAULT 0,
  succeeded INT NOT NULL DEFAULT 0,
  failed INT NOT NULL DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  finished_at TIMESTAMP NULL,
  INDEX idx_import_status (status, created_at)
);

CREATE TABLE IF NOT EXISTS import_items (
  id CHAR(36) PRIMARY KEY,
  job_id CHAR(36) NOT NULL,
  candidate_name VARCHAR(128) DEFAULT '',
  candidate_email VARCHAR(256) DEFAULT '',
  external_id VARCHAR(128) NULL,
  resume_path VARCHAR(512) NOT NULL,
  resume_sha256 CHAR(64) DEFAULT '',
  application_id CHAR(36) NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  error_message TEXT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_import_item_job (job_id, status),
  CONSTRAINT fk_import_item_job FOREIGN KEY (job_id) REFERENCES import_jobs(id) ON DELETE CASCADE
);
