USE hr_agent;

ALTER TABLE question_bank
  ADD COLUMN reference_answer TEXT NULL AFTER content,
  ADD COLUMN scoring_points_json JSON NULL AFTER reference_answer;

UPDATE question_bank SET reference_answer = '' WHERE reference_answer IS NULL;
