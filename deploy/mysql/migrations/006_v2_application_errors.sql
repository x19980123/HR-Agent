-- v2.0: business vs system errors, screen tier
USE hr_agent;

ALTER TABLE applications
  ADD COLUMN error_kind VARCHAR(16) NOT NULL DEFAULT 'none' AFTER error_message,
  ADD COLUMN human_reason_code VARCHAR(64) NULL AFTER error_kind,
  ADD COLUMN system_error_code VARCHAR(64) NULL AFTER human_reason_code,
  ADD COLUMN screen_tier VARCHAR(32) NULL AFTER system_error_code;

CREATE INDEX idx_app_error_kind ON applications (error_kind, status);
