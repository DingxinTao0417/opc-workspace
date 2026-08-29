CREATE UNIQUE INDEX idx_client_activities_project_workflow_event_source
ON client_activities(source_type, source_id)
WHERE kind = 'system_reference'
  AND source_type = 'project_workflow_event';
