-- Development/test rollback only. Production uses forward repair.
SET FOREIGN_KEY_CHECKS=0;
DROP TABLE IF EXISTS audit_logs,cost_ledger,budget_reservations,budget_accounts,inbox_events,outbox_events,job_attempts,jobs,workflows;
DROP TABLE IF EXISTS task_acceptance_items,task_events,optimization_tasks,report_artifacts,reports,metric_values,metric_snapshots;
DROP TABLE IF EXISTS citations,brand_mentions,answer_evaluations,evaluation_runs,ai_responses,ai_requests,audit_samples,audits,protocol_engines,measurement_protocols,engine_configs;
DROP TABLE IF EXISTS cluster_members,query_clusters,questions,question_set_versions,question_sets,knowledge_snapshots,entity_evidence,entity_relations,entity_aliases,entities;
DROP TABLE IF EXISTS content_signals,content_analysis_runs,findings,structured_data_items,page_links,fetch_attempts,page_contents,website_pages,crawl_urls,crawl_runs,websites;
DROP TABLE IF EXISTS competitors,brand_context_items,products,brand_aliases,brands,project_config_versions,project_memberships,projects;
DROP TABLE IF EXISTS api_tokens,service_accounts,invitations,memberships,users,tenants;
SET FOREIGN_KEY_CHECKS=1;
