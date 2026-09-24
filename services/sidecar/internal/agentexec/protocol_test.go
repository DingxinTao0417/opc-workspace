package agentexec

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func validProtocolInput() InputFrame {
	return InputFrame{
		ProtocolVersion: ProtocolVersion,
		RunID:           "018f0000-0000-7000-8000-00000000a001",
		Nonce:           "nonce-1",
		Capabilities:    []string{CapabilityReadTaskSnapshot, CapabilityWriteTextResult},
		Input:           TaskSnapshot{TaskID: "task-1", Title: "写周报", Status: "todo", Kind: "delivery"},
		ModelEndpoint:   "http://127.0.0.1:9/v1/chat/completions",
		Model:           "local-test",
		MaxResultBytes:  MaxResultBytes,
		Instruction:     "产出交付文本",
	}
}

func validFrozenFile(id, content string) FrozenFileInput {
	digest := sha256.Sum256([]byte(content))
	return FrozenFileInput{
		ID: id, SourceKind: FileSourceTaskArtifact, Name: "requirements.md", MIME: "text/markdown",
		SizeBytes: len(content), SHA256: hex.EncodeToString(digest[:]), Content: content,
	}
}

func TestValidateInputFramePreservesLegacyTextContract(t *testing.T) {
	input := validProtocolInput()
	if err := ValidateInputFrame(input); err != nil {
		t.Fatalf("ValidateInputFrame() error = %v", err)
	}
	if contract := EffectiveOutputContract(input); !reflect.DeepEqual(contract, OutputContract{Type: ResultTypeText}) {
		t.Fatalf("EffectiveOutputContract() = %#v", contract)
	}
}

func TestValidateInputFrameAcceptsFrozenFilesAndFileOutput(t *testing.T) {
	input := validProtocolInput()
	input.Files = []FrozenFileInput{
		validFrozenFile("018f0000-0000-7000-8000-00000000b001", "第一份资料"),
		validFrozenFile("018f0000-0000-7000-8000-00000000b002", "second reference"),
	}
	input.Files[1].SourceKind = FileSourceProjectAttachment
	input.OutputContract = &OutputContract{Type: ResultTypeFile, Name: "answer.md", MIME: "text/markdown"}
	input.Capabilities = []string{CapabilityReadTaskSnapshot, CapabilityReadControlledFiles, CapabilityWriteFileResult}
	if err := ValidateInputFrame(input); err != nil {
		t.Fatalf("ValidateInputFrame() error = %v", err)
	}
}

func TestMultiFileOutputContractBindsBodiesByFrozenOrder(t *testing.T) {
	contract := OutputContract{
		Type: ResultTypeFiles,
		Files: []OutputFileContract{
			{Name: "report.md", MIME: "text/markdown"},
			{Name: "facts.json", MIME: "application/json"},
		},
	}
	input := validProtocolInput()
	input.OutputContract = &contract
	input.Capabilities = []string{CapabilityReadTaskSnapshot, CapabilityWriteFilesResult}
	if err := ValidateInputFrame(input); err != nil {
		t.Fatalf("ValidateInputFrame() error = %v", err)
	}
	payload, err := ResultPayload(Result{
		Type:  ResultTypeFiles,
		Files: []string{"# 报告", `{"ok":true}`},
	}, contract, MaxResultBytes)
	if err != nil || payload != `["# 报告","{\"ok\":true}"]` {
		t.Fatalf("ResultPayload() payload=%q err=%v", payload, err)
	}
	files, err := DecodeFilesPayload(payload, contract, MaxResultBytes)
	if err != nil || !reflect.DeepEqual(files, []string{"# 报告", `{"ok":true}`}) {
		t.Fatalf("DecodeFilesPayload() files=%#v err=%v", files, err)
	}
	if _, err := ResultPayload(Result{Type: ResultTypeFiles, Files: []string{"one"}}, contract, MaxResultBytes); err == nil {
		t.Fatal("ResultPayload() accepted a missing frozen file body")
	}
	if _, err := DecodeFilesPayload(`["# 报告","{\"ok\":true}"] `, contract, MaxResultBytes); err == nil {
		t.Fatal("DecodeFilesPayload() accepted a noncanonical payload")
	}
}

func TestValidateInputFrameRequiresCapabilitiesForFilesAndOutput(t *testing.T) {
	file := validFrozenFile("018f0000-0000-7000-8000-00000000b001", "hello")
	tests := []struct {
		name         string
		files        []FrozenFileInput
		output       *OutputContract
		capabilities []string
	}{
		{
			name: "controlled file read", files: []FrozenFileInput{file},
			capabilities: []string{CapabilityReadTaskSnapshot, CapabilityWriteTextResult},
		},
		{
			name: "file result write", output: &OutputContract{Type: ResultTypeFile, Name: "answer.md", MIME: "text/markdown"},
			capabilities: []string{CapabilityReadTaskSnapshot, CapabilityWriteTextResult},
		},
		{
			name:         "text result write",
			output:       &OutputContract{Type: ResultTypeText},
			capabilities: []string{CapabilityReadTaskSnapshot, CapabilityWriteFileResult},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validProtocolInput()
			input.Files = test.files
			input.OutputContract = test.output
			input.Capabilities = test.capabilities
			if err := ValidateInputFrame(input); err == nil {
				t.Fatal("ValidateInputFrame() error = nil")
			}
		})
	}
}

func TestValidateInputFrameRejectsInvalidFrozenFiles(t *testing.T) {
	validID := "018f0000-0000-7000-8000-00000000b001"
	tests := []struct {
		name   string
		mutate func(*InputFrame)
	}{
		{name: "count", mutate: func(input *InputFrame) {
			for index := 0; index < MaxFileInputs+1; index++ {
				id := "018f0000-0000-7000-8000-00000000b00" + string(rune('1'+index))
				input.Files = append(input.Files, validFrozenFile(id, "x"))
			}
		}},
		{name: "individual size", mutate: func(input *InputFrame) {
			input.Files = []FrozenFileInput{validFrozenFile(validID, strings.Repeat("x", MaxFileInputBytes+1))}
		}},
		{name: "total size", mutate: func(input *InputFrame) {
			for index, id := range []string{
				"018f0000-0000-7000-8000-00000000b001",
				"018f0000-0000-7000-8000-00000000b002",
				"018f0000-0000-7000-8000-00000000b003",
			} {
				input.Files = append(input.Files, validFrozenFile(id, strings.Repeat(string(rune('a'+index)), 44<<10)))
			}
		}},
		{name: "hash mismatch", mutate: func(input *InputFrame) {
			file := validFrozenFile(validID, "hello")
			file.SHA256 = strings.Repeat("0", 64)
			input.Files = []FrozenFileInput{file}
		}},
		{name: "uppercase hash", mutate: func(input *InputFrame) {
			file := validFrozenFile(validID, "hello")
			file.SHA256 = strings.ToUpper(file.SHA256)
			input.Files = []FrozenFileInput{file}
		}},
		{name: "declared size", mutate: func(input *InputFrame) {
			file := validFrozenFile(validID, "hello")
			file.SizeBytes++
			input.Files = []FrozenFileInput{file}
		}},
		{name: "invalid UTF-8", mutate: func(input *InputFrame) {
			file := validFrozenFile(validID, "hello")
			file.Content = string([]byte{0xff})
			file.SizeBytes = 1
			digest := sha256.Sum256([]byte(file.Content))
			file.SHA256 = hex.EncodeToString(digest[:])
			input.Files = []FrozenFileInput{file}
		}},
		{name: "NUL", mutate: func(input *InputFrame) {
			input.Files = []FrozenFileInput{validFrozenFile(validID, "hello\x00world")}
		}},
		{name: "unsafe name", mutate: func(input *InputFrame) {
			file := validFrozenFile(validID, "hello")
			file.Name = "../secret.md"
			input.Files = []FrozenFileInput{file}
		}},
		{name: "mime", mutate: func(input *InputFrame) {
			file := validFrozenFile(validID, "hello")
			file.MIME = "application/octet-stream"
			input.Files = []FrozenFileInput{file}
		}},
		{name: "extension mismatch", mutate: func(input *InputFrame) {
			file := validFrozenFile(validID, "hello")
			file.Name = "requirements.exe"
			input.Files = []FrozenFileInput{file}
		}},
		{name: "id", mutate: func(input *InputFrame) {
			file := validFrozenFile("not-an-id", "hello")
			input.Files = []FrozenFileInput{file}
		}},
		{name: "source kind", mutate: func(input *InputFrame) {
			file := validFrozenFile(validID, "hello")
			file.SourceKind = "absolute_path"
			input.Files = []FrozenFileInput{file}
		}},
		{name: "duplicate id", mutate: func(input *InputFrame) {
			input.Files = []FrozenFileInput{validFrozenFile(validID, "one"), validFrozenFile(validID, "two")}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validProtocolInput()
			test.mutate(&input)
			if err := ValidateInputFrame(input); err == nil {
				t.Fatal("ValidateInputFrame() error = nil")
			}
		})
	}
}

func TestValidateInputFrameRejectsInvalidOutputContracts(t *testing.T) {
	tests := []OutputContract{
		{Type: "archive"},
		{Type: ResultTypeText, Name: "answer.txt"},
		{Type: ResultTypeFile, Name: "", MIME: "text/plain"},
		{Type: ResultTypeFile, Name: "../answer.md", MIME: "text/markdown"},
		{Type: ResultTypeFile, Name: "answer.md", MIME: "application/octet-stream"},
		{Type: ResultTypeFile, Name: "answer.json", MIME: "text/markdown"},
	}
	for _, contract := range tests {
		input := validProtocolInput()
		input.OutputContract = &contract
		if err := ValidateInputFrame(input); err == nil {
			t.Fatalf("ValidateInputFrame(%#v) error = nil", contract)
		}
	}
}

func TestValidateInputFrameErrorsNeverEchoFileContent(t *testing.T) {
	const secret = "DO_NOT_ECHO_PRIVATE_FILE_BODY_7f3f"
	input := validProtocolInput()
	file := validFrozenFile("018f0000-0000-7000-8000-00000000b001", secret)
	file.SHA256 = strings.Repeat("0", 64)
	input.Files = []FrozenFileInput{file}
	err := ValidateInputFrame(input)
	if err == nil {
		t.Fatal("ValidateInputFrame() error = nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("validation error leaked file body: %q", err)
	}
}

func TestReadFrameRejectsUnknownControlledFileAndOutputFields(t *testing.T) {
	input := validProtocolInput()
	input.Files = []FrozenFileInput{validFrozenFile("018f0000-0000-7000-8000-00000000b001", "hello")}
	input.OutputContract = &OutputContract{Type: ResultTypeFile, Name: "answer.md", MIME: "text/markdown"}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}

	for _, mutate := range []func(map[string]any){
		func(frame map[string]any) { frame["files"].([]any)[0].(map[string]any)["path"] = "C:/secret.txt" },
		func(frame map[string]any) { frame["output_contract"].(map[string]any)["path"] = "C:/output.md" },
	} {
		var copyFrame map[string]any
		copyBytes, _ := json.Marshal(decoded)
		_ = json.Unmarshal(copyBytes, &copyFrame)
		mutate(copyFrame)
		var wire bytes.Buffer
		if err := WriteFrame(&wire, copyFrame); err != nil {
			t.Fatal(err)
		}
		var target InputFrame
		if err := ReadFrame(bufio.NewReader(&wire), &target); err == nil {
			t.Fatal("ReadFrame() accepted an unknown nested field")
		}
	}
}
