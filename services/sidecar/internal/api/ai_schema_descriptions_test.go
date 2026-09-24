package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
)

func TestAIProposeSchemaKeepsTaskAndProjectDescriptionFields(t *testing.T) {
	tool := &aiWorkspaceTool{name: "workspace_propose", policy: harness.NewCapabilities("work", "actions")}
	var schema map[string]any
	if err := json.Unmarshal(tool.InputSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	changes := properties["changes"].(map[string]any)
	fields := changes["properties"].(map[string]any)
	if !reflect.DeepEqual(fields["description"], map[string]any{"type": "string", "maxLength": float64(10000)}) {
		t.Fatalf("Task/Project description was removed from advertised changes: %#v", fields["description"])
	}
	for _, action := range []string{"task.create", "task.update", "project.create", "project.update"} {
		found := false
		for _, value := range properties["action"].(map[string]any)["enum"].([]any) {
			if value == action {
				found = true
			}
		}
		if !found {
			t.Fatalf("fixture did not authorize %s", action)
		}
		changes := map[string]any{"description": "完整保留的任务或项目说明"}
		input := map[string]any{"action": action, "changes": changes}
		if action == "task.create" {
			changes["title"] = "带说明任务"
		} else if action == "project.create" {
			changes["name"] = "带说明项目"
		} else {
			input["expected_version"] = 1
			if action == "task.update" {
				input["task_id"] = "018f0000-0000-7000-8000-000000000111"
			} else {
				input["project_id"] = "018f0000-0000-7000-8000-000000000112"
			}
		}
		encoded, _ := json.Marshal(input)
		if _, err := parseAIWorkspaceAction(encoded); err != nil {
			t.Fatalf("advertised %s description is not accepted by its real parser: %v", action, err)
		}
	}
	for _, value := range properties["action"].(map[string]any)["enum"].([]any) {
		action := value.(string)
		if strings.HasPrefix(action, "agent_run.") || strings.HasPrefix(action, "financial_entry.") || strings.HasPrefix(action, "invoice.") || strings.HasPrefix(action, "knowledge_") || action == "finance.export_csv" {
			t.Fatalf("description restoration widened workspace action authority: %s", action)
		}
	}
}

func TestOmitModelSchemaDescriptionsPreservesNestedSchemaConstraints(t *testing.T) {
	for _, keyword := range []string{"items", "additionalProperties", "additionalItems", "unevaluatedProperties", "unevaluatedItems", "contains", "propertyNames", "not", "if", "then", "else", "contentSchema", "allOf", "anyOf", "oneOf", "prefixItems", "tuple_items"} {
		t.Run(keyword, func(t *testing.T) {
			child := map[string]any{
				"type": "object", "description": "schema prose", "additionalProperties": false,
				"required":   []any{"description"},
				"properties": map[string]any{"description": map[string]any{"type": "string", "minLength": float64(1), "maxLength": float64(10000), "description": "field prose"}},
				"enum":       []any{map[string]any{"description": "unchanged data"}},
			}
			expected := map[string]any{
				"type": "object", "additionalProperties": false,
				"required":   []any{"description"},
				"properties": map[string]any{"description": map[string]any{"type": "string", "minLength": float64(1), "maxLength": float64(10000)}},
				"enum":       []any{map[string]any{"description": "unchanged data"}},
			}
			input := map[string]any{keyword: any(child), "description": "outer prose"}
			want := map[string]any{keyword: any(expected)}
			switch keyword {
			case "allOf", "anyOf", "oneOf", "prefixItems":
				input[keyword], want[keyword] = []any{child, true, false}, []any{expected, true, false}
			case "tuple_items":
				delete(input, keyword)
				delete(want, keyword)
				input["items"], want["items"] = []any{child, true, false}, []any{expected, true, false}
			}
			before, _ := json.Marshal(input)
			if got := omitModelSchemaDescriptions(input); !reflect.DeepEqual(got, want) {
				t.Fatalf("schema constraints or instance data changed: got=%#v want=%#v", got, want)
			}
			if after, _ := json.Marshal(input); string(before) != string(after) {
				t.Fatal("input schema changed")
			}
		})
	}
}

func TestOmitModelSchemaDescriptionsPreservesLegacyDependencies(t *testing.T) {
	input := map[string]any{"dependencies": map[string]any{
		"description": []any{"description", "title"},
		"title":       map[string]any{"description": "dependency schema prose", "required": []any{"description"}},
	}}
	want := map[string]any{"dependencies": map[string]any{
		"description": []any{"description", "title"},
		"title":       map[string]any{"required": []any{"description"}},
	}}
	if got := omitModelSchemaDescriptions(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy dependency form changed: %#v", got)
	}
}

func TestOmitModelSchemaDescriptionsCopiesTypedBuilderLists(t *testing.T) {
	input := map[string]any{"enum": []string{"description", "original"}}
	output := omitModelSchemaDescriptions(input).(map[string]any)
	output["enum"].([]string)[1] = "changed copy"
	if input["enum"].([]string)[1] != "original" {
		t.Fatal("a typed list added by a schema builder still aliases its source")
	}
}

func TestOmitModelSchemaDescriptionsPreservesEmptyAndNilInstanceValues(t *testing.T) {
	for name, value := range map[string]any{
		"empty_typed_list": []string{},
		"nil_typed_list":   []string(nil),
		"empty_list":       []any{},
		"nil_list":         []any(nil),
		"empty_object":     map[string]any{},
		"nil_object":       map[string]any(nil),
	} {
		t.Run(name, func(t *testing.T) {
			input := map[string]any{"default": value}
			before, _ := json.Marshal(input)
			after, _ := json.Marshal(omitModelSchemaDescriptions(input))
			if string(before) != string(after) {
				t.Fatalf("instance shape changed: before=%s after=%s", before, after)
			}
		})
	}
}

func TestOmitModelSchemaDescriptionsKeepsSchemaDictionariesAndInstanceValues(t *testing.T) {
	var input map[string]any
	if err := json.Unmarshal([]byte(`{
 "type":"object","description":"schema prose","required":["description"],
 "additionalProperties":false,
 "properties":{"description":{"type":"string","minLength":2,"maxLength":20,"description":"field prose"}},
 "definitions":{"description":{"type":"object","description":"definition prose","properties":{"description":{"type":"integer","minimum":1}}}},
 "$defs":{"description":{"type":"string","pattern":"^[a-z]+$","description":"named schema prose"}},
 "patternProperties":{"description":{"type":"boolean","description":"pattern prose"}},
 "dependentSchemas":{"description":{"required":["description"],"description":"dependency prose"}},
 "enum":[{"description":"actual enum data","nested":{"description":"retain"}}],
 "const":{"description":"actual const data"},
 "default":{"description":"actual default data"},
 "examples":[{"description":"actual example data"}],
 "x-custom":{"description":"opaque extension data"}
}`), &input); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(input)
	output := omitModelSchemaDescriptions(input).(map[string]any)
	if after, _ := json.Marshal(input); string(after) != string(before) {
		t.Fatal("schema optimization mutated its input")
	}
	if _, exists := output["description"]; exists {
		t.Fatal("root schema annotation was not removed")
	}
	for _, dictionary := range []string{"properties", "definitions", "$defs", "patternProperties", "dependentSchemas"} {
		entry, ok := output[dictionary].(map[string]any)["description"].(map[string]any)
		if !ok {
			t.Fatalf("schema dictionary %s lost a named description entry", dictionary)
		}
		if _, exists := entry["description"]; exists {
			t.Fatalf("schema annotation was retained in %s", dictionary)
		}
	}
	if output["enum"].([]any)[0].(map[string]any)["description"] != "actual enum data" || output["const"].(map[string]any)["description"] != "actual const data" || output["default"].(map[string]any)["description"] != "actual default data" || output["examples"].([]any)[0].(map[string]any)["description"] != "actual example data" || output["x-custom"].(map[string]any)["description"] != "opaque extension data" {
		t.Fatal("instance or extension data was incorrectly treated as schema prose")
	}
	output["properties"].(map[string]any)["description"].(map[string]any)["minLength"] = float64(999)
	output["enum"].([]any)[0].(map[string]any)["nested"].(map[string]any)["description"] = "mutated output"
	if after, _ := json.Marshal(input); string(after) != string(before) {
		t.Fatal("optimized schemas or copied instance data still alias their input")
	}
}
