USE hr_agent;

-- Company-level interviewer catalog (not login users). Classification via role_kinds + specialties.
CREATE TABLE IF NOT EXISTS interviewer_profiles (
    open_id         VARCHAR(128) PRIMARY KEY,
    name            VARCHAR(128) NOT NULL DEFAULT '',
    email           VARCHAR(255) NOT NULL DEFAULT '',
    department      VARCHAR(128) NOT NULL DEFAULT '',
    role_kinds      JSON NOT NULL,          -- e.g. ["tech","hm"]
    specialties     JSON NULL,              -- e.g. ["backend","go"]
    enabled         TINYINT(1) NOT NULL DEFAULT 1,
    notes           VARCHAR(512) NOT NULL DEFAULT '',
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_ip_dept (department, enabled)
);

CREATE TABLE IF NOT EXISTS interviewer_pools (
    id                  VARCHAR(64) PRIMARY KEY,
    name                VARCHAR(128) NOT NULL,
    default_role_kind   VARCHAR(32) NOT NULL DEFAULT 'tech',
    department          VARCHAR(128) NOT NULL DEFAULT '',
    enabled             TINYINT(1) NOT NULL DEFAULT 1,
    notes               VARCHAR(512) NOT NULL DEFAULT '',
    created_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS interviewer_pool_members (
    pool_id     VARCHAR(64) NOT NULL,
    open_id     VARCHAR(128) NOT NULL,
    role_kind   VARCHAR(32) NOT NULL DEFAULT '',  -- empty => use pool default_role_kind
    PRIMARY KEY (pool_id, open_id),
    INDEX idx_ipm_open (open_id),
    CONSTRAINT fk_ipm_pool FOREIGN KEY (pool_id) REFERENCES interviewer_pools(id) ON DELETE CASCADE,
    CONSTRAINT fk_ipm_profile FOREIGN KEY (open_id) REFERENCES interviewer_profiles(open_id) ON DELETE CASCADE
);
