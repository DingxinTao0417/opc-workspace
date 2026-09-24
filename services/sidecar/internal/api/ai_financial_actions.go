package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func addAIFinancialActionSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "financial_entry.create", "financial_entry.update", "financial_entry.void")
	properties["financial_entry_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Actual ledger ID from workspace_finance; required with expected_version for update/void only"}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Financial entry create/update/void require finance + finance_actions, independently of work/actions. Create requires explicit type, amount_minor, currency, occurred_on, status, category. Update only supplied ledger fields. Void only reason (1-1000 chars), excludes record from totals without deletion or refund; separate HUMAN financial consent required for ALL three commands. No invoice-linked or voided entry edits. Ask for actual facts; never infer receipt/payment from intent. Amount is a positive integer minor unit; no conversion. Notes replacement must be supplied by user, old notes are not available to model. Optional client_id/project_id use real UUID/null; native project client inference appears in HUMAN preview. No actor/invoice/consent overrides, external payment, invoice actions, PDF or export."
	f := changes["properties"].(map[string]any)
	f["type"] = map[string]any{"type": "string", "enum": []string{"income", "expense"}}
	f["amount_minor"] = map[string]any{"type": "integer", "minimum": 1, "maximum": maxFinancialAmountMinor}
	f["currency"] = map[string]any{"type": "string", "pattern": "^[A-Z]{3}$"}
	f["occurred_on"] = map[string]any{"type": "string", "description": "Actual stored calendar date YYYY-MM-DD; ask if unknown"}
	f["status"] = map[string]any{"type": "string", "enum": []string{"pending", "confirmed"}}
	f["category"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 80}
	f["notes"].(map[string]any)["maxLength"] = 10000
}

// omitModelSchemaDescriptions copies a JSON Schema while removing only schema
// annotations. Property/definition names are not schema keywords, and values of
// enum/const/default/examples or unknown extensions are instance data. A generic
// recursive delete would silently remove the Task/Project description field.
func omitModelSchemaDescriptions(value any) any {
	schema, ok := value.(map[string]any)
	if !ok {
		return copyModelSchemaValue(value)
	}
	result := make(map[string]any, len(schema))
	for key, child := range schema {
		if key == "description" {
			continue
		}
		switch key {
		case "properties", "patternProperties", "definitions", "$defs", "dependentSchemas", "dependencies":
			if entries, ok := child.(map[string]any); ok {
				copied := make(map[string]any, len(entries))
				for name, entry := range entries {
					// Legacy dependencies also permits arrays of property names;
					// those are copied as data, never walked as schemas.
					copied[name] = omitModelSchemaDescriptions(entry)
				}
				result[key] = copied
			} else {
				result[key] = copyModelSchemaValue(child)
			}
		case "allOf", "anyOf", "oneOf", "prefixItems":
			if entries, ok := child.([]any); ok {
				copied := make([]any, len(entries))
				for i, entry := range entries {
					copied[i] = omitModelSchemaDescriptions(entry)
				}
				result[key] = copied
			} else {
				result[key] = copyModelSchemaValue(child)
			}
		case "items":
			if entries, ok := child.([]any); ok {
				// Draft-07 and earlier allow tuple schemas under items.
				copied := make([]any, len(entries))
				for i, entry := range entries {
					copied[i] = omitModelSchemaDescriptions(entry)
				}
				result[key] = copied
			} else {
				result[key] = omitModelSchemaDescriptions(child)
			}
		case "additionalProperties", "additionalItems", "unevaluatedProperties", "unevaluatedItems", "contains", "propertyNames", "not", "if", "then", "else", "contentSchema":
			result[key] = omitModelSchemaDescriptions(child)
		default:
			result[key] = copyModelSchemaValue(child)
		}
	}
	return result
}

func copyModelSchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		if typed == nil {
			return typed
		}
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = copyModelSchemaValue(child)
		}
		return result
	case []any:
		if typed == nil {
			return typed
		}
		result := make([]any, len(typed))
		for i, child := range typed {
			result[i] = copyModelSchemaValue(child)
		}
		return result
	case []string:
		// Schema builders can add typed enum/required lists after unmarshalling.
		if typed == nil {
			return typed
		}
		result := make([]string, len(typed))
		copy(result, typed)
		return result
	default:
		return value
	}
}

func (t *aiWorkspaceTool) financialScopedActionSchema() json.RawMessage {
	var root map[string]any
	_ = json.Unmarshal(aiWorkspaceActionSchema(), &root)
	properties := root["properties"].(map[string]any)
	if t.policy.Allows("agent_files") {
		addAIAgentRunFileSchema(properties)
	}
	action := properties["action"].(map[string]any)
	allowed := []any{}
	for _, value := range action["enum"].([]any) {
		actionName := value.(string)
		if actionName == "finance.export_csv" {
			if t.policy.Allows("finance") && t.policy.Allows("finance_exports") {
				allowed = append(allowed, value)
			}
			continue
		}
		financial := strings.HasPrefix(actionName, "financial_entry.")
		invoice := strings.HasPrefix(actionName, "invoice.")
		agentExecution := strings.HasPrefix(actionName, "agent_run.")
		knowledge := isAIKnowledgeAction(actionName)
		if (agentExecution && t.policy.Allows("work") && t.policy.Allows("outputs") && t.policy.Allows("actions") && t.policy.Allows("agent_execution")) || (invoice && t.policy.Allows("finance") && t.policy.Allows("invoice_actions")) || (financial && t.policy.Allows("finance") && t.policy.Allows("finance_actions")) || (knowledge && t.policy.Allows("knowledge_actions")) || (!agentExecution && !invoice && !financial && !knowledge && t.policy.Allows("work") && t.policy.Allows("actions")) {
			allowed = append(allowed, value)
		}
	}
	action["enum"] = allowed
	// aiWorkspaceActionSchema deliberately keeps long domain documentation for
	// focused tests and maintainers. The model can read those rules through the
	// scope-filtered workspace_guide and the tool's own description, so copying
	// every prose description into workspace_propose would crowd real user text
	// and bounded approval receipts out of the provider's 64 KiB request. Omit
	// prose only from this scoped, model-facing copy; all machine-enforced fields,
	// enums, formats, ranges and additionalProperties boundaries remain intact.
	encoded, _ := json.Marshal(omitModelSchemaDescriptions(root))
	return encoded
}

func parseAIFinancialAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for key := range raw {
		if key != "action" && key != "financial_entry_id" && key != "expected_version" && key != "changes" {
			return input, errors.New("financial actions only accept their ledger target")
		}
	}
	creating := input.Action == "financial_entry.create"
	if !creating && input.Action != "financial_entry.update" && input.Action != "financial_entry.void" {
		return input, errors.New("unsupported financial action")
	}
	if creating {
		if _, ok := raw["financial_entry_id"]; ok {
			return input, errors.New("create has no existing target")
		}
		if _, ok := raw["expected_version"]; ok {
			return input, errors.New("create has no existing version")
		}
	} else if id, err := uuid.Parse(input.FinancialEntryID); err != nil || id.String() != input.FinancialEntryID || input.ExpectedVersion < 1 {
		return input, errors.New("read real ledger ID/version first")
	}
	if len(fields) == 0 {
		return input, errors.New("financial changes required")
	}
	canonical := map[string]any{}
	for key, rawValue := range fields {
		if input.Action == "financial_entry.void" && key != "reason" {
			return input, errors.New("void only accepts reason")
		}
		if key == "amount_minor" {
			var n int64
			if json.Unmarshal(rawValue, &n) != nil || validateFinancialAmount(n) != nil {
				return input, errors.New("amount_minor must be a positive safe integer within the ledger limit")
			}
			canonical[key] = n
			continue
		}
		if (key == "client_id" || key == "project_id") && string(rawValue) == "null" {
			canonical[key] = nil
			continue
		}
		var s string
		if string(rawValue) == "null" || json.Unmarshal(rawValue, &s) != nil {
			return input, errors.New("invalid financial field type")
		}
		var err error
		switch key {
		case "type":
			s, err = normalizeFinancialEntryType(s)
		case "currency":
			if normalized, e := normalizeFinancialCurrency(s); e != nil || normalized != s {
				return input, errors.New("explicit uppercase currency required")
			}
		case "occurred_on":
			if !validDate(s) {
				return input, errors.New("valid actual date required")
			}
		case "status":
			if s != "pending" && s != "confirmed" {
				return input, errors.New("status must be explicit pending or confirmed")
			}
		case "category":
			s, err = normalizeFinancialCategory(s)
		case "notes":
			if utf8.RuneCountInString(s) > 10000 {
				return input, errors.New("notes too long")
			}
		case "client_id", "project_id":
			if id, e := uuid.Parse(s); e != nil || id.String() != s {
				return input, errors.New("canonical association ID required")
			}
		case "reason":
			if input.Action != "financial_entry.void" {
				return input, errors.New("reason is only for void")
			}
			s = strings.TrimSpace(s)
			err = validateFinancialVoidReason(s)
		default:
			return input, errors.New("unsupported financial field")
		}
		if err != nil {
			return input, err
		}
		canonical[key] = s
	}
	if creating {
		for _, key := range []string{"type", "amount_minor", "currency", "occurred_on", "status", "category"} {
			if _, ok := canonical[key]; !ok {
				return input, errors.New("financial create requires explicit " + key)
			}
		}
	}
	input.Changes, _ = json.Marshal(canonical)
	return input, nil
}

func aiFinancialFields(entry models.FinancialEntry) map[string]any {
	return map[string]any{"type": entry.Type, "amount_minor": entry.AmountMinor, "currency": entry.Currency, "occurred_on": entry.OccurredOn,
		"status": entry.Status, "category": entry.Category, "client_id": entry.ClientID, "project_id": entry.ProjectID, "notes": entry.Notes}
}

func previewAIFinancialAction(tx *gorm.DB, input aiWorkspaceAction, now time.Time) (aiActionPreview, error) {
	p := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	var entry models.FinancialEntry
	var err error
	if input.Action == "financial_entry.create" {
		var request createFinancialEntryRequest
		_ = json.Unmarshal(input.Changes, &request)
		entry, err = financialEntryFromCreateRequest(request, now)
		if err == nil {
			err = normalizeFinancialEntryAssociations(tx, &entry)
		}
		if err != nil {
			return p, err
		}
		p.After = aiFinancialFields(entry)
	} else {
		entry, err = prepareFinancialEntryChange(tx, input.FinancialEntryID, input.ExpectedVersion)
		if err != nil {
			return p, err
		}
		p.Before, p.After = aiFinancialFields(entry), aiFinancialFields(entry)
		if input.Action == "financial_entry.void" {
			var request voidFinancialEntryRequest
			_ = json.Unmarshal(input.Changes, &request)
			p.After["status"], p.After["reason"] = "voided", request.Reason
		} else {
			var request updateFinancialEntryRequest
			_ = json.Unmarshal(input.Changes, &request)
			updates, e := financialEntryUpdates(tx, entry, request)
			if e != nil {
				return p, e
			}
			for key, value := range updates {
				p.After[key] = value
			}
		}
	}
	// Names and complete notes stay in the local immutable HUMAN preview only.
	// Re-reading at confirmation protects inferred project/client relationships.
	for _, fields := range []map[string]any{p.Before, p.After} {
		if len(fields) == 0 {
			continue
		}
		for _, relation := range []struct{ id, name, table string }{{"client_id", "client_name", "clients"}, {"project_id", "project_name", "projects"}} {
			fields[relation.name] = nil
			var id string
			switch v := fields[relation.id].(type) {
			case string:
				id = v
			case *string:
				if v != nil {
					id = *v
				}
			}
			if id == "" {
				continue
			}
			var row struct{ Name string }
			if e := tx.Table(relation.table).Select("name").Where("id=?", id).Take(&row).Error; e != nil {
				return p, e
			}
			fields[relation.name] = row.Name
		}
	}
	p.Label = fmt.Sprintf("%s · %s · %s", p.After["occurred_on"], p.After["currency"], p.After["category"])
	return p, nil
}

func executeAIFinancialAction(tx *gorm.DB, input aiWorkspaceAction, requestID, now string) (aiActionResult, error) {
	var out financialEntryResponse
	var err error
	switch input.Action {
	case "financial_entry.create":
		var request createFinancialEntryRequest
		_ = json.Unmarshal(input.Changes, &request)
		clock, e := time.Parse(time.RFC3339Nano, now)
		if e != nil {
			return aiActionResult{}, e
		}
		entry, e := financialEntryFromCreateRequest(request, clock)
		if e != nil {
			return aiActionResult{}, e
		}
		out, err = createFinancialEntryInTransaction(tx, entry, requestID)
	case "financial_entry.update":
		var request updateFinancialEntryRequest
		_ = json.Unmarshal(input.Changes, &request)
		out, err = updateFinancialEntryInTransaction(tx, input.FinancialEntryID, input.ExpectedVersion, request, requestID, now)
	case "financial_entry.void":
		var request voidFinancialEntryRequest
		_ = json.Unmarshal(input.Changes, &request)
		out, err = voidFinancialEntryInTransaction(tx, input.FinancialEntryID, input.ExpectedVersion, request.Reason, requestID, now)
	default:
		return aiActionResult{}, errors.New("unsupported financial action")
	}
	return aiActionResult{ID: out.ID, Version: out.Version}, err
}
