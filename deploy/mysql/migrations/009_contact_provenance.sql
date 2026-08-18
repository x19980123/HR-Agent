USE hr_agent;

ALTER TABLE applications
  ADD COLUMN contact_email_source VARCHAR(32) NULL AFTER candidate_email,
  ADD COLUMN contact_email_confidence DECIMAL(4,3) NULL AFTER contact_email_source,
  ADD COLUMN contact_resolved_at DATETIME NULL AFTER contact_email_confidence;

ALTER TABLE import_items
  ADD COLUMN email_source VARCHAR(32) NULL AFTER candidate_email,
  ADD COLUMN pre_extract_email VARCHAR(255) NULL AFTER email_source;
