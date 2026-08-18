USE hr_agent;

ALTER TABLE staff_members
  ADD COLUMN can_manage_question_bank TINYINT(1) NOT NULL DEFAULT 0 AFTER is_admin;

-- seed system admin may manage question bank
UPDATE staff_members SET can_manage_question_bank=1 WHERE is_admin=1;
