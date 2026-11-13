-- FEAT-12: task discussion comments.
CREATE TABLE task_comments (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  task_id BINARY(16) NOT NULL,
  author_id BINARY(16) NOT NULL,
  body TEXT NOT NULL,
  created_at DATETIME(6) NOT NULL,
  KEY ix_task_comments (task_id,created_at),
  CONSTRAINT fk_task_comment_task FOREIGN KEY (task_id) REFERENCES optimization_tasks(id)
) ENGINE=InnoDB;
