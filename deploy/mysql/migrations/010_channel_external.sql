USE hr_agent;

ALTER TABLE applications
  ADD COLUMN channel VARCHAR(32) NULL AFTER jd_id,
  ADD COLUMN external_id VARCHAR(128) NULL AFTER channel;

ALTER TABLE import_items
  ADD COLUMN pre_extract_meta JSON NULL AFTER pre_extract_email;

CREATE INDEX idx_app_jd_external ON applications (jd_id, external_id);
