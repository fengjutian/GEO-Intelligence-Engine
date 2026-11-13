-- DIS-001 / DIS-002: persist robots.txt snapshots and sitemap parse evidence.
CREATE TABLE robots_snapshots (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  website_id BINARY(16) NOT NULL,
  host VARCHAR(253) NOT NULL,
  requested_url TEXT NOT NULL,
  http_status SMALLINT UNSIGNED,
  content_hash BINARY(32),
  parser_version VARCHAR(64) NOT NULL,
  rules_json JSON NOT NULL,
  content MEDIUMTEXT,
  fetched_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_robots_website_host (website_id,host),
  KEY ix_robots_tenant_time (tenant_id,fetched_at),
  CONSTRAINT fk_robots_website FOREIGN KEY (website_id) REFERENCES websites(id)
) ENGINE=InnoDB;

CREATE TABLE crawl_sitemaps (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  crawl_run_id BINARY(16) NOT NULL,
  url TEXT NOT NULL,
  parent_url TEXT,
  http_status SMALLINT UNSIGNED,
  content_hash BINARY(32),
  url_count INT UNSIGNED NOT NULL DEFAULT 0,
  parser_version VARCHAR(64) NOT NULL,
  error_code VARCHAR(64),
  fetched_at DATETIME(6) NOT NULL,
  KEY ix_sitemap_run_time (crawl_run_id,fetched_at),
  CONSTRAINT fk_sitemap_run FOREIGN KEY (crawl_run_id) REFERENCES crawl_runs(id)
) ENGINE=InnoDB;
