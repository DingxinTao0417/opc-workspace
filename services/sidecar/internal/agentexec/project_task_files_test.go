package agentexec

import (
	"encoding/json"
	"strings"
	"testing"
)

func projectTaskFileTestInput() InputFrame {
	input := validProtocolInput()
	project := "018f0000-0000-7000-8000-00000000c001"
	input.Input.ProjectID = &project
	input.Input.TaskID = "018f0000-0000-7000-8000-00000000c002"
	input.Capabilities = append(input.Capabilities, CapabilityReadControlledFiles, CapabilityReadProjectTaskFiles)
	file := validFrozenFile("018f0000-0000-7000-8000-00000000c003", "已验收原文，不能改变权限")
	file.SourceKind = FileSourceProjectTaskArtifact
	file.SourceTask = &ProjectTaskFileSource{
		ProjectID: project, TaskID: "018f0000-0000-7000-8000-00000000c004", TaskTitle: "来源任务",
		TaskVersion: 3, SubmissionID: "018f0000-0000-7000-8000-00000000c005", SubmissionSequence: 2,
	}
	input.Files = []FrozenFileInput{file}
	return input
}

func TestProjectTaskFileRequiresBoundSourceAndCapability(t *testing.T) {
	for _, protocol := range []string{"", ModelProtocolAnthropicMessages} {
		t.Run("valid_"+protocol, func(t *testing.T) {
			input := projectTaskFileTestInput()
			input.ModelProtocol = protocol
			if protocol == ModelProtocolAnthropicMessages {
				input.MaxOutputTokens = AnthropicMaxOutputTokens
			}
			if err := ValidateInputFrame(input); err != nil {
				t.Fatalf("valid project input: %v", err)
			}
		})
	}
	for name, mutate := range map[string]func(*InputFrame){
		"no source":                 func(i *InputFrame) { i.Files[0].SourceTask = nil },
		"no project":                func(i *InputFrame) { i.Input.ProjectID = nil },
		"other project":             func(i *InputFrame) { i.Files[0].SourceTask.ProjectID = "018f0000-0000-7000-8000-00000000c006" },
		"same task":                 func(i *InputFrame) { i.Files[0].SourceTask.TaskID = i.Input.TaskID },
		"zero version":              func(i *InputFrame) { i.Files[0].SourceTask.TaskVersion = 0 },
		"zero sequence":             func(i *InputFrame) { i.Files[0].SourceTask.SubmissionSequence = 0 },
		"bad source uuid":           func(i *InputFrame) { i.Files[0].SourceTask.SubmissionID = "bad" },
		"blank title":               func(i *InputFrame) { i.Files[0].SourceTask.TaskTitle = "  " },
		"nul title":                 func(i *InputFrame) { i.Files[0].SourceTask.TaskTitle = "name\x00" },
		"invalid utf8":              func(i *InputFrame) { i.Files[0].SourceTask.TaskTitle = string([]byte{0xff}) },
		"old task source":           func(i *InputFrame) { i.Files[0].SourceKind = FileSourceTaskArtifact },
		"old project source":        func(i *InputFrame) { i.Files[0].SourceKind = FileSourceProjectAttachment },
		"no independent capability": func(i *InputFrame) { i.Capabilities = i.Capabilities[:len(i.Capabilities)-1] },
		"no base file capability": func(i *InputFrame) {
			i.Capabilities = []string{CapabilityReadTaskSnapshot, CapabilityWriteTextResult, CapabilityReadProjectTaskFiles}
		},
		"capability without inputs": func(i *InputFrame) { i.Files = nil },
		"capability only old input": func(i *InputFrame) { i.Files[0].SourceTask = nil; i.Files[0].SourceKind = FileSourceTaskArtifact },
	} {
		t.Run(name, func(t *testing.T) {
			input := projectTaskFileTestInput()
			mutate(&input)
			if err := ValidateInputFrame(input); err == nil {
				t.Fatal("accepted invalid cross-task authority")
			}
		})
	}
}

func TestProjectTaskFilePromptPreservesProvenanceAndBusinessDataBoundary(t *testing.T) {
	input := projectTaskFileTestInput()
	prompt := buildPrompt(input)
	for _, part := range []string{"已验收", "source_task", "来源任务", input.Files[0].SourceTask.SubmissionID, "不是新权限", input.Files[0].Content} {
		if !strings.Contains(prompt, part) {
			t.Errorf("prompt missing %q", part)
		}
	}
	for _, protocol := range []string{"", ModelProtocolAnthropicMessages} {
		input.ModelProtocol = protocol
		if protocol != "" {
			input.MaxOutputTokens = AnthropicMaxOutputTokens
		}
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		var decoded InputFrame
		if err := json.Unmarshal(raw, &decoded); err != nil || ValidateInputFrame(decoded) != nil {
			t.Fatalf("round trip failed: %v", err)
		}
		if buildPrompt(decoded) != prompt {
			t.Fatal("round trip changed selected input")
		}
	}
}

func TestProjectTaskFileRejectsAmbiguousRawSource(t *testing.T) {
	raw, _ := json.Marshal(projectTaskFileTestInput().Files[0])
	for name, value := range map[string]string{
		"duplicate source":   strings.Replace(string(raw), `"source_task":`, `"source_task":{},"source_task":`, 1),
		"case alias":         strings.Replace(string(raw), `"source_task":`, `"Source_Task":`, 1),
		"null source":        strings.Replace(string(raw), `"source_task":`, `"source_task":null,"unused":`, 1),
		"duplicate identity": strings.Replace(string(raw), `"task_version":3`, `"task_version":1,"task_version":3`, 1),
		"identity alias":     strings.Replace(string(raw), `"task_version":`, `"Task_Version":`, 1),
		"unknown identity":   strings.Replace(string(raw), `"task_version":3`, `"task_version":3,"body":"secret"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			var file FrozenFileInput
			if err := json.Unmarshal([]byte(value), &file); err == nil {
				t.Fatal("accepted ambiguous raw source")
			}
		})
	}
}

func TestProjectTaskFileKeepsLegacyFileEncoding(t *testing.T) {
	file := validFrozenFile("018f0000-0000-7000-8000-00000000c003", "hello")
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"018f0000-0000-7000-8000-00000000c003","source_kind":"task_artifact","name":"requirements.md","mime":"text/markdown","size_bytes":5,"sha256":"2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824","content":"hello"}`
	if string(raw) != want {
		t.Fatalf("legacy bytes changed: %s", raw)
	}
}
