package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentRunOutputDeliveryMigrationBackfillsExactLinksAndRetainsLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-run-output-delivery.db")
	old := openDatabaseAtVersion(t, path, 74)
	const (
		taskID             = "018f0000-0000-7000-8000-000000007501"
		linkedTaskID       = "018f0000-0000-7000-8000-000000007514"
		adapterID          = "018f0000-0000-7000-8000-000000007502"
		actorID            = "018f0000-0000-7000-8000-000000007503"
		assignmentID       = "018f0000-0000-7000-8000-000000007504"
		linkedAssignID     = "018f0000-0000-7000-8000-000000007515"
		providerID         = "018f0000-0000-7000-8000-000000007505"
		submittedRun       = "018f0000-0000-7000-8000-000000007506"
		legacyRun          = "018f0000-0000-7000-8000-000000007507"
		submissionID       = "018f0000-0000-7000-8000-000000007508"
		artifactID         = "018f0000-0000-7000-8000-000000007509"
		mismatchedRun      = "018f0000-0000-7000-8000-000000007512"
		oversizedRun       = "018f0000-0000-7000-8000-000000007513"
		linkedOversizedRun = "018f0000-0000-7000-8000-000000007516"
		linkedSubmissionID = "018f0000-0000-7000-8000-000000007517"
		linkedArtifactID   = "018f0000-0000-7000-8000-000000007518"
		now                = "2026-09-18T12:00:00Z"
	)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO agent_adapters(
			id,adapter_key,kind,display_name,executable_ref,manifest_json,protocol_version,
			status,health_status,isolation_status,execution_ready,last_health_at,version,created_at,updated_at
		) VALUES (?, 'delivery-migration', 'builtin', 'Delivery Agent', 'builtin:delivery', '{}',
			'opc-agent-pipe-v1', 'enabled', 'healthy', 'verified', 1, ?, 1, ?, ?)`, []any{adapterID, now, now, now}},
		{`INSERT INTO actors(id,type,display_name,status,is_builtin,metadata_json,agent_adapter_id,version,created_at,updated_at)
			VALUES (?, 'agent', 'Delivery Agent', 'active', 0, '{}', ?, 1, ?, ?)`, []any{actorID, adapterID, now, now}},
		{`INSERT INTO tasks(id,title,status,review_policy,created_at,updated_at)
			VALUES (?, 'Historical Agent output', 'todo', 'manual', ?, ?)`, []any{taskID, now, now}},
		{`INSERT INTO tasks(id,title,status,review_policy,created_at,updated_at)
			VALUES (?, 'Linked oversized Agent output', 'todo', 'manual', ?, ?)`, []any{linkedTaskID, now, now}},
		{`INSERT INTO task_assignments(id,task_id,actor_id,role,assigned_by_actor_id,assigned_at)
			VALUES (?, ?, ?, 'assignee', '00000000-0000-5000-8000-000000000001', ?)`, []any{assignmentID, taskID, actorID, now}},
		{`INSERT INTO task_assignments(id,task_id,actor_id,role,assigned_by_actor_id,assigned_at)
			VALUES (?, ?, ?, 'assignee', '00000000-0000-5000-8000-000000000001', ?)`, []any{linkedAssignID, linkedTaskID, actorID, now}},
		{`INSERT INTO task_submissions(
			id,task_id,sequence,status,summary,submitted_by_actor_id,submitted_at,origin
		) VALUES (?, ?, 1, 'pending_review', 'Historical Agent output', ?, ?, 'manual')`, []any{submissionID, taskID, actorID, now}},
		{`INSERT INTO task_submissions(
			id,task_id,sequence,status,summary,submitted_by_actor_id,submitted_at,origin
		) VALUES (?, ?, 1, 'pending_review', 'Linked oversized Agent output', ?, ?, 'manual')`, []any{linkedSubmissionID, linkedTaskID, actorID, now}},
		{`INSERT INTO task_artifacts(
			id,task_id,submission_id,position,storage_kind,name,content_text,
			produced_by_actor_id,recorded_by_actor_id,integrity_status,created_at
		) VALUES (?, ?, ?, 1, 'text', 'agent-run-attempt-1.md', 'historical result', ?,
			'00000000-0000-5000-8000-000000000002', 'unverified', ?)`, []any{artifactID, taskID, submissionID, actorID, now}},
		{`INSERT INTO task_artifacts(
			id,task_id,submission_id,position,storage_kind,name,content_text,
			produced_by_actor_id,recorded_by_actor_id,integrity_status,created_at
		) VALUES (?, ?, ?, 1, 'text', 'agent-run-attempt-1.md', 'complete linked historical output', ?,
			'00000000-0000-5000-8000-000000000002', 'unverified', ?)`, []any{linkedArtifactID, linkedTaskID, linkedSubmissionID, actorID, now}},
	}
	for _, statement := range statements {
		if _, err := old.Exec(statement.query, statement.args...); err != nil {
			_ = old.Close()
			t.Fatalf("seed output delivery migration: %v", err)
		}
	}
	insertRun := `INSERT INTO agent_runs(
		id,task_id,assignment_id,actor_id,adapter_id,created_by_actor_id,attempt,status,
		provider_id,model,input_snapshot_json,result_text,result_bytes,started_at,completed_at,created_at,
		task_version,assignment_assigned_at,actor_version,adapter_version,provider_version,
		provider_config_version,execution_contract_version
	) VALUES (?, ?, ?, ?, ?, '00000000-0000-5000-8000-000000000001', ?, 'succeeded',
		?, 'historical-model', '{}', ?, ?, ?, ?, ?, 1, ?, 1, 1, 1, 1, 1)`
	if _, err := old.Exec(insertRun, submittedRun, taskID, assignmentID, actorID, adapterID, 1,
		providerID, "historical result", len("historical result"), now, now, now, now); err != nil {
		_ = old.Close()
		t.Fatalf("insert linked historical Run: %v", err)
	}
	if _, err := old.Exec(insertRun, legacyRun, taskID, assignmentID, actorID, adapterID, 2,
		providerID, "legacy result", len("legacy result"), now, now, now, now); err != nil {
		_ = old.Close()
		t.Fatalf("insert unlinked historical Run: %v", err)
	}
	if _, err := old.Exec(insertRun, mismatchedRun, taskID, assignmentID, actorID, adapterID, 3,
		providerID, "你好", 2, now, now, now, now); err != nil {
		_ = old.Close()
		t.Fatalf("insert historical Run with character byte count: %v", err)
	}
	oversizedResult := strings.Repeat("你", 30000)
	if _, err := old.Exec(insertRun, oversizedRun, taskID, assignmentID, actorID, adapterID, 4,
		providerID, oversizedResult, len(oversizedResult), now, now, now, now); err != nil {
		_ = old.Close()
		t.Fatalf("insert oversized historical Run: %v", err)
	}
	if _, err := old.Exec(insertRun, linkedOversizedRun, linkedTaskID, linkedAssignID, actorID, adapterID, 1,
		providerID, oversizedResult, len(oversizedResult), now, now, now, now); err != nil {
		_ = old.Close()
		t.Fatalf("insert linked oversized historical Run: %v", err)
	}
	if _, err := old.Exec(`INSERT INTO workflow_events(
		id,aggregate_type,aggregate_id,action,actor_id,current_json,created_at
	) VALUES ('018f0000-0000-7000-8000-000000007510','agent_run',?,'agent_run_output_submitted',
		'00000000-0000-5000-8000-000000000002',json_object('submission_id',?,'artifact_id',?),?)`,
		submittedRun, submissionID, artifactID, now); err != nil {
		_ = old.Close()
		t.Fatalf("insert immutable output event: %v", err)
	}
	if _, err := old.Exec(`INSERT INTO workflow_events(
		id,aggregate_type,aggregate_id,action,actor_id,current_json,created_at
	) VALUES ('018f0000-0000-7000-8000-000000007519','agent_run',?,'agent_run_output_submitted',
		'00000000-0000-5000-8000-000000000002',json_object('submission_id',?,'artifact_id',?),?)`,
		linkedOversizedRun, linkedSubmissionID, linkedArtifactID, now); err != nil {
		_ = old.Close()
		t.Fatalf("insert linked oversized output event: %v", err)
	}
	// Simulate a corrupt legacy event imported before current validation. The
	// migration must sanitize JSON before calling json_type/json_extract.
	if _, err := old.Exec("PRAGMA ignore_check_constraints = ON"); err != nil {
		_ = old.Close()
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO workflow_events(
		id,aggregate_type,aggregate_id,action,actor_id,current_json,created_at
	) VALUES ('018f0000-0000-7000-8000-000000007511','agent_run',?,'agent_run_output_submitted',
		'00000000-0000-5000-8000-000000000002','{broken',?)`, legacyRun, now); err != nil {
		_ = old.Close()
		t.Fatalf("insert corrupt legacy event: %v", err)
	}
	if _, err := old.Exec("PRAGMA ignore_check_constraints = OFF"); err != nil {
		_ = old.Close()
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade v74 output delivery database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 79 {
		t.Fatalf("SchemaVersion=%d, want 79", store.SchemaVersion)
	}
	var rows []struct {
		ID                      string
		Status                  string
		ResultText              *string
		ResultBytes             *int
		ErrorCode               *string
		OutputDeliveryStatus    string
		OutputDeliveryErrorCode *string
		SubmissionID            *string
		ArtifactID              *string
	}
	if err := store.DB.Table("agent_runs").Where("id IN ?", []string{
		submittedRun, legacyRun, mismatchedRun, oversizedRun, linkedOversizedRun,
	}).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]struct {
		ID                      string
		Status                  string
		ResultText              *string
		ResultBytes             *int
		ErrorCode               *string
		OutputDeliveryStatus    string
		OutputDeliveryErrorCode *string
		SubmissionID            *string
		ArtifactID              *string
	}, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	linked := byID[submittedRun]
	if linked.OutputDeliveryStatus != "submitted" || linked.OutputDeliveryErrorCode != nil ||
		linked.SubmissionID == nil || *linked.SubmissionID != submissionID ||
		linked.ArtifactID == nil || *linked.ArtifactID != artifactID {
		t.Fatalf("linked historical Run = %#v", linked)
	}
	legacy := byID[legacyRun]
	if legacy.OutputDeliveryStatus != "retained" || legacy.OutputDeliveryErrorCode == nil ||
		*legacy.OutputDeliveryErrorCode != "AGENT_OUTPUT_DELIVERY_LEGACY" ||
		legacy.SubmissionID != nil || legacy.ArtifactID != nil {
		t.Fatalf("legacy historical Run = %#v", legacy)
	}
	mismatched := byID[mismatchedRun]
	if mismatched.Status != "succeeded" || mismatched.ResultText == nil || *mismatched.ResultText != "你好" ||
		mismatched.ResultBytes == nil || *mismatched.ResultBytes != len("你好") ||
		mismatched.OutputDeliveryStatus != "retained" || mismatched.OutputDeliveryErrorCode == nil ||
		*mismatched.OutputDeliveryErrorCode != "AGENT_OUTPUT_DELIVERY_LEGACY" {
		t.Fatalf("normalized historical Run = %#v", mismatched)
	}
	oversized := byID[oversizedRun]
	if oversized.Status != "failed" || oversized.ResultText != nil || oversized.ResultBytes != nil ||
		oversized.ErrorCode == nil || *oversized.ErrorCode != "AGENT_RESULT_TOO_LARGE" ||
		oversized.OutputDeliveryStatus != "not_ready" || oversized.OutputDeliveryErrorCode != nil ||
		oversized.SubmissionID != nil || oversized.ArtifactID != nil {
		t.Fatalf("safely disposed oversized historical Run = %#v", oversized)
	}
	linkedOversized := byID[linkedOversizedRun]
	const normalizedLinkedResult = "Historical Agent Run result omitted during schema 075 normalization; use the linked Task Artifact."
	if linkedOversized.Status != "succeeded" || linkedOversized.ResultText == nil ||
		*linkedOversized.ResultText != normalizedLinkedResult || linkedOversized.ResultBytes == nil ||
		*linkedOversized.ResultBytes != len(normalizedLinkedResult) ||
		linkedOversized.OutputDeliveryStatus != "submitted" || linkedOversized.OutputDeliveryErrorCode != nil ||
		linkedOversized.SubmissionID == nil || *linkedOversized.SubmissionID != linkedSubmissionID ||
		linkedOversized.ArtifactID == nil || *linkedOversized.ArtifactID != linkedArtifactID {
		t.Fatalf("linked oversized historical Run = %#v", linkedOversized)
	}
}

func TestAgentRunOutputDeliveryMigrationEnforcesStateCombinations(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent-run-output-delivery-constraints.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const (
		taskID       = "018f0000-0000-7000-8000-000000007521"
		adapterID    = "018f0000-0000-7000-8000-000000007522"
		actorID      = "018f0000-0000-7000-8000-000000007523"
		assignmentID = "018f0000-0000-7000-8000-000000007524"
		providerID   = "018f0000-0000-7000-8000-000000007525"
		runID        = "018f0000-0000-7000-8000-000000007526"
		submissionID = "018f0000-0000-7000-8000-000000007527"
		artifactID   = "018f0000-0000-7000-8000-000000007528"
		now          = "2026-09-18T12:00:00Z"
	)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO agent_adapters(
			id,adapter_key,kind,display_name,executable_ref,manifest_json,protocol_version,
			status,health_status,isolation_status,execution_ready,last_health_at,version,created_at,updated_at
		) VALUES (?, 'delivery-constraints', 'builtin', 'Delivery Agent', 'builtin:delivery', '{}',
			'opc-agent-pipe-v1', 'enabled', 'healthy', 'verified', 1, ?, 1, ?, ?)`, []any{adapterID, now, now, now}},
		{`INSERT INTO actors(id,type,display_name,status,is_builtin,metadata_json,agent_adapter_id,version,created_at,updated_at)
			VALUES (?, 'agent', 'Delivery Agent', 'active', 0, '{}', ?, 1, ?, ?)`, []any{actorID, adapterID, now, now}},
		{`INSERT INTO tasks(id,title,status,review_policy,created_at,updated_at)
			VALUES (?, 'Pending delivery constraints', 'todo', 'manual', ?, ?)`, []any{taskID, now, now}},
		{`INSERT INTO task_assignments(id,task_id,actor_id,role,assigned_by_actor_id,assigned_at)
			VALUES (?, ?, ?, 'assignee', '00000000-0000-5000-8000-000000000001', ?)`, []any{assignmentID, taskID, actorID, now}},
		{`INSERT INTO task_submissions(
			id,task_id,sequence,status,summary,submitted_by_actor_id,submitted_at,origin
		) VALUES (?, ?, 1, 'pending_review', 'Constraint fixture', ?, ?, 'manual')`, []any{submissionID, taskID, actorID, now}},
		{`INSERT INTO task_artifacts(
			id,task_id,submission_id,position,storage_kind,name,content_text,
			produced_by_actor_id,recorded_by_actor_id,integrity_status,created_at
		) VALUES (?, ?, ?, 1, 'text', 'constraint.md', 'abc', ?,
			'00000000-0000-5000-8000-000000000002', 'unverified', ?)`, []any{artifactID, taskID, submissionID, actorID, now}},
		{`INSERT INTO agent_runs(
			id,task_id,assignment_id,actor_id,adapter_id,created_by_actor_id,attempt,status,
			provider_id,model,input_snapshot_json,started_at,created_at,task_version,assignment_assigned_at,
			actor_version,adapter_version,provider_version,provider_config_version,execution_contract_version,
			output_delivery_status
		) VALUES (?, ?, ?, ?, ?, '00000000-0000-5000-8000-000000000001', 1, 'running',
			?, 'model', '{}', ?, ?, 1, ?, 1, 1, 1, 1, 1, 'not_ready')`, []any{runID, taskID, assignmentID, actorID, adapterID, providerID, now, now, now}},
	} {
		if err := store.DB.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("seed state constraint: %v", err)
		}
	}
	invalidUpdates := []string{
		`UPDATE agent_runs SET output_delivery_status='pending',
		 output_delivery_error_code=NULL, output_delivery_pending_text='abc',
		 output_delivery_pending_bytes=3, output_delivery_pending_completed_at='2026-09-18T12:01:00Z'
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET output_delivery_status='pending',
		 output_delivery_error_code='AGENT_OUTPUT_DELIVERY_PENDING', output_delivery_pending_text=NULL,
		 output_delivery_pending_bytes=3, output_delivery_pending_completed_at='2026-09-18T12:01:00Z'
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET output_delivery_status='pending',
		 output_delivery_error_code='AGENT_OUTPUT_DELIVERY_PENDING', output_delivery_pending_text='abc',
		 output_delivery_pending_bytes=NULL, output_delivery_pending_completed_at='2026-09-18T12:01:00Z'
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET output_delivery_status='pending',
		 output_delivery_error_code='AGENT_OUTPUT_DELIVERY_PENDING', output_delivery_pending_text='abc',
		 output_delivery_pending_bytes=3, output_delivery_pending_completed_at=NULL
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET output_delivery_status='pending',
		 output_delivery_error_code='WRONG', output_delivery_pending_text='abc',
		 output_delivery_pending_bytes=3, output_delivery_pending_completed_at='2026-09-18T12:01:00Z'
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET output_delivery_status='pending',
		 output_delivery_error_code='AGENT_OUTPUT_DELIVERY_PENDING', output_delivery_pending_text='abc',
		 output_delivery_pending_bytes=2, output_delivery_pending_completed_at='2026-09-18T12:01:00Z'
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET status='succeeded', result_text='abc', result_bytes=3,
		 completed_at='2026-09-18T12:01:00Z', output_delivery_status='pending',
		 output_delivery_error_code='AGENT_OUTPUT_DELIVERY_PENDING', output_delivery_pending_text='abc',
		 output_delivery_pending_bytes=3, output_delivery_pending_completed_at='2026-09-18T12:01:00Z'
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET status='succeeded', result_text='abc', result_bytes=3,
		 completed_at='2026-09-18T12:01:00Z', output_delivery_status='retained',
		 output_delivery_error_code=NULL WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET status='succeeded', result_text=NULL, result_bytes=3,
		 completed_at='2026-09-18T12:01:00Z', output_delivery_status='retained',
		 output_delivery_error_code='AGENT_OUTPUT_DELIVERY_IDENTITY_CHANGED'
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET status='succeeded', result_text='abc', result_bytes=NULL,
		 completed_at='2026-09-18T12:01:00Z', output_delivery_status='retained',
		 output_delivery_error_code='AGENT_OUTPUT_DELIVERY_IDENTITY_CHANGED'
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET status='succeeded', result_text='abc', result_bytes=3,
		 completed_at='2026-09-18T12:01:00Z', output_delivery_status='submitted',
		 submission_id=NULL, artifact_id=NULL WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET status='succeeded', result_text=NULL, result_bytes=3,
		 completed_at='2026-09-18T12:01:00Z', output_delivery_status='submitted',
		 submission_id='018f0000-0000-7000-8000-000000007527',
		 artifact_id='018f0000-0000-7000-8000-000000007528'
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET status='succeeded', result_text='abc', result_bytes=NULL,
		 completed_at='2026-09-18T12:01:00Z', output_delivery_status='submitted',
		 submission_id='018f0000-0000-7000-8000-000000007527',
		 artifact_id='018f0000-0000-7000-8000-000000007528'
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
	}
	for _, query := range invalidUpdates {
		if err := store.DB.Exec(query).Error; err == nil || !strings.Contains(err.Error(), "AGENT_RUN_OUTPUT_DELIVERY_INVALID") {
			t.Fatalf("invalid output delivery update error=%v", err)
		}
	}
	oversizedResult := strings.Repeat("你", 30000)
	if err := store.DB.Exec(`UPDATE agent_runs SET status='succeeded', result_text=?, result_bytes=?,
		completed_at=?, output_delivery_status='retained',
		output_delivery_error_code='AGENT_OUTPUT_DELIVERY_IDENTITY_CHANGED' WHERE id=?`,
		oversizedResult, len(oversizedResult), now, runID).Error; err == nil ||
		!strings.Contains(err.Error(), "AGENT_RUN_OUTPUT_DELIVERY_INVALID") {
		t.Fatalf("oversized output delivery update error=%v", err)
	}
	if err := store.DB.Exec(`UPDATE agent_runs SET output_delivery_status='pending',
		output_delivery_error_code='AGENT_OUTPUT_DELIVERY_PENDING', output_delivery_pending_text='你好',
		output_delivery_pending_bytes=6, output_delivery_pending_completed_at=? WHERE id=?`, now, runID).Error; err != nil {
		t.Fatalf("valid pending output: %v", err)
	}
	var pending struct {
		Status                  string
		OutputDeliveryStatus    string
		OutputDeliveryErrorCode *string
	}
	if err := store.DB.Table("agent_runs").Where("id = ?", runID).Take(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending.Status != "running" || pending.OutputDeliveryStatus != "pending" ||
		pending.OutputDeliveryErrorCode == nil || *pending.OutputDeliveryErrorCode != "AGENT_OUTPUT_DELIVERY_PENDING" {
		t.Fatalf("valid pending row = %#v", pending)
	}
	if err := store.DB.Exec(`UPDATE agent_runs SET output_delivery_status=output_delivery_status WHERE id=?`, runID).Error; err != nil {
		t.Fatalf("idempotent pending observation update: %v", err)
	}
	pendingMutations := []string{
		`UPDATE agent_runs SET status='interrupted', completed_at='2026-09-18T12:01:00Z',
		 output_delivery_status='not_ready', output_delivery_error_code=NULL,
		 output_delivery_pending_text=NULL, output_delivery_pending_bytes=NULL,
		 output_delivery_pending_completed_at=NULL
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET output_delivery_pending_text='再见', output_delivery_pending_bytes=6
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET output_delivery_pending_completed_at='2026-09-18T12:02:00Z'
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
		`UPDATE agent_runs SET status='succeeded', result_text='changed', result_bytes=7,
		 completed_at='2026-09-18T12:00:00Z', output_delivery_status='retained',
		 output_delivery_error_code='AGENT_RUN_IDENTITY_CHANGED',
		 output_delivery_pending_text=NULL, output_delivery_pending_bytes=NULL,
		 output_delivery_pending_completed_at=NULL
		 WHERE id='018f0000-0000-7000-8000-000000007526'`,
	}
	for _, query := range pendingMutations {
		if err := store.DB.Exec(query).Error; err == nil ||
			!strings.Contains(err.Error(), "AGENT_RUN_OUTPUT_DELIVERY_PENDING_IMMUTABLE") {
			t.Fatalf("pending delivery mutation error=%v query=%s", err, query)
		}
	}
	invalidInserts := []struct {
		name             string
		id               string
		attempt          int
		status           string
		resultText       any
		resultBytes      any
		completedAt      any
		deliveryStatus   string
		deliveryError    any
		submissionID     any
		artifactID       any
		pendingText      any
		pendingBytes     any
		pendingCompleted any
	}{
		{name: "pending null error", id: "018f0000-0000-7000-8000-000000007531", attempt: 2, status: "running", deliveryStatus: "pending", pendingText: "abc", pendingBytes: 3, pendingCompleted: now},
		{name: "pending null text", id: "018f0000-0000-7000-8000-000000007532", attempt: 3, status: "running", deliveryStatus: "pending", deliveryError: "AGENT_OUTPUT_DELIVERY_PENDING", pendingBytes: 3, pendingCompleted: now},
		{name: "pending null bytes", id: "018f0000-0000-7000-8000-000000007533", attempt: 4, status: "running", deliveryStatus: "pending", deliveryError: "AGENT_OUTPUT_DELIVERY_PENDING", pendingText: "abc", pendingCompleted: now},
		{name: "pending null completion", id: "018f0000-0000-7000-8000-000000007534", attempt: 5, status: "running", deliveryStatus: "pending", deliveryError: "AGENT_OUTPUT_DELIVERY_PENDING", pendingText: "abc", pendingBytes: 3},
		{name: "submitted null result", id: "018f0000-0000-7000-8000-000000007535", attempt: 6, status: "succeeded", resultBytes: 3, completedAt: now, deliveryStatus: "submitted", submissionID: submissionID, artifactID: artifactID},
		{name: "submitted null result bytes", id: "018f0000-0000-7000-8000-000000007536", attempt: 7, status: "succeeded", resultText: "abc", completedAt: now, deliveryStatus: "submitted", submissionID: submissionID, artifactID: artifactID},
		{name: "submitted null identities", id: "018f0000-0000-7000-8000-000000007537", attempt: 8, status: "succeeded", resultText: "abc", resultBytes: 3, completedAt: now, deliveryStatus: "submitted"},
		{name: "retained null result", id: "018f0000-0000-7000-8000-000000007538", attempt: 9, status: "succeeded", resultBytes: 3, completedAt: now, deliveryStatus: "retained", deliveryError: "AGENT_RUN_IDENTITY_CHANGED"},
		{name: "retained null result bytes", id: "018f0000-0000-7000-8000-000000007539", attempt: 10, status: "succeeded", resultText: "abc", completedAt: now, deliveryStatus: "retained", deliveryError: "AGENT_RUN_IDENTITY_CHANGED"},
		{name: "retained null error", id: "018f0000-0000-7000-8000-000000007540", attempt: 11, status: "succeeded", resultText: "abc", resultBytes: 3, completedAt: now, deliveryStatus: "retained"},
		{name: "retained oversized utf8 result", id: "018f0000-0000-7000-8000-000000007541", attempt: 12, status: "succeeded", resultText: oversizedResult, resultBytes: len(oversizedResult), completedAt: now, deliveryStatus: "retained", deliveryError: "AGENT_RUN_IDENTITY_CHANGED"},
	}
	const insertRun = `INSERT INTO agent_runs(
		id,task_id,assignment_id,actor_id,adapter_id,created_by_actor_id,attempt,status,
		provider_id,model,input_snapshot_json,result_text,result_bytes,started_at,completed_at,created_at,
		task_version,assignment_assigned_at,actor_version,adapter_version,provider_version,
		provider_config_version,execution_contract_version,output_delivery_status,
		output_delivery_error_code,submission_id,artifact_id,output_delivery_pending_text,
		output_delivery_pending_bytes,output_delivery_pending_completed_at
	) VALUES (?, ?, ?, ?, ?, '00000000-0000-5000-8000-000000000001', ?, ?,
		?, 'model', '{}', ?, ?, ?, ?, ?, 1, ?, 1, 1, 1, 1, 1, ?, ?, ?, ?, ?, ?, ?)`
	for _, test := range invalidInserts {
		t.Run("insert "+test.name, func(t *testing.T) {
			err := store.DB.Exec(insertRun,
				test.id, taskID, assignmentID, actorID, adapterID, test.attempt, test.status,
				providerID, test.resultText, test.resultBytes, now, test.completedAt, now, now,
				test.deliveryStatus, test.deliveryError, test.submissionID, test.artifactID,
				test.pendingText, test.pendingBytes, test.pendingCompleted,
			).Error
			if err == nil || !strings.Contains(err.Error(), "AGENT_RUN_OUTPUT_DELIVERY_INVALID") {
				t.Fatalf("invalid output delivery insert error=%v", err)
			}
		})
	}
	if err := store.DB.Exec(`UPDATE agent_runs SET status='succeeded', result_text='你好', result_bytes=6,
		completed_at=?, output_delivery_status='retained',
		output_delivery_error_code='AGENT_RUN_IDENTITY_CHANGED',
		output_delivery_pending_text=NULL, output_delivery_pending_bytes=NULL,
		output_delivery_pending_completed_at=NULL WHERE id=?`, now, runID).Error; err != nil {
		t.Fatalf("consume pending output into retained terminal state: %v", err)
	}
	var consumed struct {
		Status               string
		ResultText           *string
		ResultBytes          *int
		OutputDeliveryStatus string
	}
	if err := store.DB.Table("agent_runs").Where("id = ?", runID).Take(&consumed).Error; err != nil {
		t.Fatal(err)
	}
	if consumed.Status != "succeeded" || consumed.ResultText == nil || *consumed.ResultText != "你好" ||
		consumed.ResultBytes == nil || *consumed.ResultBytes != 6 ||
		consumed.OutputDeliveryStatus != "retained" {
		t.Fatalf("consumed pending row = %#v", consumed)
	}

	const terminalWithoutOutput = `INSERT INTO agent_runs(
		id,task_id,assignment_id,actor_id,adapter_id,created_by_actor_id,parent_run_id,attempt,status,
		provider_id,model,input_snapshot_json,error_code,started_at,completed_at,created_at,
		task_version,assignment_assigned_at,actor_version,adapter_version,provider_version,
		provider_config_version,execution_contract_version,output_delivery_status
	) VALUES (?, ?, ?, ?, ?, '00000000-0000-5000-8000-000000000001', ?, ?, ?,
		?, 'model', '{}', ?, ?, ?, ?, 1, ?, 1, 1, 1, 1, 1, 'not_ready')`
	parentID := "018f0000-0000-7000-8000-000000007550"
	childID := "018f0000-0000-7000-8000-000000007551"
	cancelledID := "018f0000-0000-7000-8000-000000007552"
	interruptedID := "018f0000-0000-7000-8000-000000007553"
	for _, terminal := range []struct {
		id       string
		parentID any
		attempt  int
		status   string
		error    any
	}{
		{id: parentID, attempt: 20, status: "failed", error: "AGENT_RUN_FAILED"},
		{id: childID, parentID: parentID, attempt: 21, status: "failed", error: "AGENT_RUN_FAILED"},
		{id: cancelledID, attempt: 22, status: "cancelled"},
		{id: interruptedID, attempt: 23, status: "interrupted"},
	} {
		if err := store.DB.Exec(terminalWithoutOutput,
			terminal.id, taskID, assignmentID, actorID, adapterID, terminal.parentID,
			terminal.attempt, terminal.status, providerID, terminal.error, now, now, now, now,
		).Error; err != nil {
			t.Fatalf("insert terminal %s Run: %v", terminal.status, err)
		}
	}
	succeededID := "018f0000-0000-7000-8000-000000007554"
	if err := store.DB.Exec(`INSERT INTO agent_runs(
		id,task_id,assignment_id,actor_id,adapter_id,created_by_actor_id,attempt,status,
		provider_id,model,input_snapshot_json,result_text,result_bytes,started_at,completed_at,created_at,
		task_version,assignment_assigned_at,actor_version,adapter_version,provider_version,
		provider_config_version,execution_contract_version,output_delivery_status,output_delivery_error_code
	) VALUES (?, ?, ?, ?, ?, '00000000-0000-5000-8000-000000000001', 24, 'succeeded',
		?, 'model', '{}', 'abc', 3, ?, ?, ?, 1, ?, 1, 1, 1, 1, 1, 'retained',
		'AGENT_OUTPUT_DELIVERY_LEGACY')`, succeededID, taskID, assignmentID, actorID, adapterID,
		providerID, now, now, now, now).Error; err != nil {
		t.Fatalf("insert succeeded Run for immutability: %v", err)
	}
	immutableUpdates := []string{
		`UPDATE agent_runs SET status='queued', error_code=NULL, started_at=NULL, completed_at=NULL
		 WHERE id='018f0000-0000-7000-8000-000000007550'`,
		`UPDATE agent_runs SET status='succeeded', error_code=NULL, result_text='abc', result_bytes=3,
		 output_delivery_status='retained', output_delivery_error_code='AGENT_OUTPUT_DELIVERY_LEGACY'
		 WHERE id='018f0000-0000-7000-8000-000000007550'`,
		`UPDATE agent_runs SET status='queued', started_at=NULL, completed_at=NULL
		 WHERE id='018f0000-0000-7000-8000-000000007552'`,
		`UPDATE agent_runs SET status='cancelled'
		 WHERE id='018f0000-0000-7000-8000-000000007553'`,
		`UPDATE agent_runs SET model='different-model'
		 WHERE id='018f0000-0000-7000-8000-000000007554'`,
		`UPDATE agent_runs SET result_text='def'
		 WHERE id='018f0000-0000-7000-8000-000000007554'`,
	}
	for _, query := range immutableUpdates {
		if err := store.DB.Exec(query).Error; err == nil ||
			!strings.Contains(err.Error(), "AGENT_RUN_OUTPUT_DELIVERY_IMMUTABLE") {
			t.Fatalf("terminal Run mutation error=%v query=%s", err, query)
		}
	}
	if err := store.DB.Exec("DELETE FROM agent_runs WHERE id = ?", parentID).Error; err != nil {
		t.Fatalf("delete terminal parent Run through FK contract: %v", err)
	}
	var childParent *string
	if err := store.DB.Raw("SELECT parent_run_id FROM agent_runs WHERE id = ?", childID).Scan(&childParent).Error; err != nil {
		t.Fatal(err)
	}
	if childParent != nil {
		t.Fatalf("terminal child parent_run_id=%v, want nil after parent deletion", *childParent)
	}
}

func TestAgentRunOutputDeliveryMigrationRequiresDestructiveAuthorization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-run-output-delivery-gate.db")
	old := openDatabaseAtVersion(t, path, 74)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	gated, gate, err := OpenBeforeDestructiveMigrations(path)
	if err != nil {
		t.Fatal(err)
	}
	if gated.SchemaVersion != 74 || gate == nil || gate.CurrentVersion != 74 || gate.TargetVersion != 79 ||
		len(gate.PendingVersions) != 5 || gate.PendingVersions[0] != 75 || gate.PendingVersions[1] != 76 || gate.PendingVersions[2] != 77 || gate.PendingVersions[3] != 78 || gate.PendingVersions[4] != 79 {
		_ = gated.Close()
		t.Fatalf("schema 075 migration gate: store=%d gate=%#v", gated.SchemaVersion, gate)
	}
	if err := gated.Close(); err != nil {
		t.Fatal(err)
	}
	authorized, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer authorized.Close()
	if authorized.SchemaVersion != 79 {
		t.Fatalf("authorized schema=%d, want 79", authorized.SchemaVersion)
	}
}
