ALTER TABLE monitoring_alerts
  ADD UNIQUE KEY uq_monitoring_alert (schedule_id,audit_id,kind);
