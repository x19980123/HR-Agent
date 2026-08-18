USE hr_agent;

-- Phase 4 MVP: offer state machine + timestamps (feedback_json already on application_interview_rounds)
ALTER TABLE applications
  ADD COLUMN offer_status VARCHAR(32) NOT NULL DEFAULT 'none' AFTER current_round_index,
  ADD COLUMN offer_note VARCHAR(1024) NOT NULL DEFAULT '' AFTER offer_status,
  ADD COLUMN offer_updated_at TIMESTAMP NULL DEFAULT NULL AFTER offer_note,
  ADD COLUMN hired_at TIMESTAMP NULL DEFAULT NULL AFTER offer_updated_at;

CREATE INDEX idx_app_offer ON applications (offer_status);
