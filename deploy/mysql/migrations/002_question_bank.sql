USE hr_agent;

CREATE TABLE IF NOT EXISTS question_bank (
    id              VARCHAR(64) PRIMARY KEY,
    category        VARCHAR(64) NOT NULL DEFAULT 'fundamentals',
    title           VARCHAR(255) DEFAULT '',
    content         TEXT NOT NULL,
    tags_json       JSON NULL,
    difficulty      VARCHAR(32) DEFAULT 'medium',
    jd_id           VARCHAR(64) NULL,
    enabled         TINYINT(1) NOT NULL DEFAULT 1,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_bank_cat (category),
    INDEX idx_bank_enabled (enabled)
);

INSERT INTO question_bank (id, category, title, content, tags_json, difficulty) VALUES
('algo-two-sum', 'algorithm', '两数之和',
 '两数之和：给定数组与目标值，返回两数下标。考察哈希表。',
 JSON_ARRAY('algorithm', 'hash', 'array'), 'medium'),
('algo-lrucache', 'algorithm', 'LRU Cache',
 '设计 LRU Cache。考察哈希 + 双向链表。',
 JSON_ARRAY('algorithm', 'design'), 'medium'),
('sys-url-shortener', 'system_design', '短链接系统',
 '设计短链接系统：生成、跳转、高并发、存储。',
 JSON_ARRAY('system_design', 'distributed'), 'hard'),
('sys-mq', 'system_design', '消息队列投递语义',
 '消息队列如何保证至少一次/恰好一次投递？',
 JSON_ARRAY('system_design', 'mq'), 'medium'),
('fund-mysql-index', 'fundamentals', 'MySQL 索引',
 'MySQL 索引结构与最左前缀原则。',
 JSON_ARRAY('fundamentals', 'mysql'), 'easy'),
('fund-go-GMP', 'fundamentals', 'Go GMP',
 '解释 Go GMP 调度模型与常见阻塞场景。',
 JSON_ARRAY('fundamentals', 'go'), 'medium')
ON DUPLICATE KEY UPDATE content=VALUES(content), tags_json=VALUES(tags_json);
