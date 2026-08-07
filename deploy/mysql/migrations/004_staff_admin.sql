USE hr_agent;

-- Add is_admin if missing (ignore duplicate column error on re-run)
SET @col := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'staff_members' AND COLUMN_NAME = 'is_admin'
);
SET @sql := IF(@col = 0,
  'ALTER TABLE staff_members ADD COLUMN is_admin TINYINT(1) NOT NULL DEFAULT 0 AFTER is_interviewer',
  'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
