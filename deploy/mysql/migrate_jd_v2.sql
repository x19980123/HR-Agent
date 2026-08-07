USE hr_agent;

ALTER TABLE job_descriptions
  ADD COLUMN IF NOT EXISTS salary VARCHAR(128) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS location VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS description TEXT NULL;

-- MySQL < 8.0.12 may not support IF NOT EXISTS on ADD COLUMN; run one-by-one if needed:
-- ALTER TABLE job_descriptions ADD COLUMN salary VARCHAR(128) NOT NULL DEFAULT '';
-- ALTER TABLE job_descriptions ADD COLUMN location VARCHAR(255) NOT NULL DEFAULT '';
-- ALTER TABLE job_descriptions ADD COLUMN description TEXT NULL;
