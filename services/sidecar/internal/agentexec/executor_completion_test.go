package agentexec

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecutorRequiresExplicitCompleteTextResponse(t *testing.T) {
	cases := []struct {
		name, response, code string
	}{
		{"complete", `{"choices":[{"finish_reason":"stop","message":{"content":"完整交付"}}]}`, ""},
		{"truncated", `{"choices":[{"finish_reason":"length","message":{"content":"部分交付"}}]}`, "AGENT_MODEL_TRUNCATED"},
		{"filtered", `{"choices":[{"finish_reason":"content_filter","message":{"content":"部分交付"}}]}`, "AGENT_MODEL_FILTERED"},
		{"refusal", `{"choices":[{"finish_reason":"stop","message":{"content":"看似交付","refusal":"private refusal"}}]}`, "AGENT_MODEL_FILTERED"},
		{"tools", `{"choices":[{"finish_reason":"tool_calls","message":{"content":"看似交付"}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"function", `{"choices":[{"finish_reason":"function_call","message":{"content":"看似交付"}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"unknown", `{"choices":[{"finish_reason":"future_stop","message":{"content":"看似交付"}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"missing", `{"choices":[{"message":{"content":"看似交付"}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"null", `{"choices":[{"finish_reason":null,"message":{"content":"看似交付"}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"empty", `{"choices":[{"finish_reason":"","message":{"content":"看似交付"}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"typed_wrong", `{"choices":[{"finish_reason":7,"message":{"content":"看似交付"}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"case_alias", `{"choices":[{"Finish_Reason":"stop","message":{"content":"看似交付"}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"no_choices", `{"choices":[]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"null_message", `{"choices":[{"finish_reason":"stop","message":null}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"null_content", `{"choices":[{"finish_reason":"stop","message":{"content":null}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"typed_refusal", `{"choices":[{"finish_reason":"stop","message":{"content":"看似交付","refusal":{}}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"typed_tools", `{"choices":[{"finish_reason":"stop","message":{"content":"看似交付","tool_calls":{}}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"trailing", `{"choices":[{"finish_reason":"stop","message":{"content":"看似交付"}}]} {}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"invalid_utf8", "{\"choices\":[{\"finish_reason\":\"stop\",\"message\":{\"content\":\"invalid\xffutf8\"}}]}", "AGENT_MODEL_RESPONSE_INVALID"},
		{"nontext", `{"choices":[{"finish_reason":"stop","message":{"content":[{"text":"text"}]}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"stop_with_tools", `{"choices":[{"finish_reason":"stop","message":{"content":"看似交付","tool_calls":[{"id":"tool"}]}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"stop_with_function", `{"choices":[{"finish_reason":"stop","message":{"content":"看似交付","function_call":{}}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"multiple_choices", `{"choices":[{"finish_reason":"stop","message":{"content":"first"}},{"finish_reason":"length","message":{"content":"second"}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"duplicate_finish", `{"choices":[{"finish_reason":"length","finish_reason":"stop","message":{"content":"部分交付"}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"complete_nullable_fields", `{"choices":[{"finish_reason":"stop","message":{"content":"完整交付","refusal":null,"tool_calls":[],"function_call":null}}]}`, ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, test.response)
			}))
			defer server.Close()
			input := validProtocolInput()
			input.ModelEndpoint = server.URL
			var stdin, stdout bytes.Buffer
			if err := WriteFrame(&stdin, input); err != nil {
				t.Fatal(err)
			}
			err := RunExecutor(&stdin, &stdout, time.Second)
			var manifest ManifestFrame
			if readErr := ReadFrame(bufio.NewReader(&stdout), &manifest); readErr != nil {
				t.Fatal(readErr)
			}
			if requests.Load() != 1 {
				t.Fatalf("requests = %d, want exactly one", requests.Load())
			}
			if manifest.RunID != input.RunID || manifest.Nonce != input.Nonce {
				t.Fatal("identity changed")
			}
			if test.code == "" {
				if err != nil || manifest.Result.Type != ResultTypeText || manifest.Result.Text != "完整交付" {
					t.Fatalf("result = %#v, err = %v", manifest.Result, err)
				}
			} else if err == nil || manifest.Result.Type != "error" || manifest.Result.Text != test.code {
				t.Fatalf("result = %#v, err = %v; want safe error %s", manifest.Result, err, test.code)
			}
		})
	}
}

func TestExecutorMainSafeFailureUsesCleanExit(t *testing.T) {
	if os.Getenv("OPC_TEST_EXECUTOR_MAIN") == "1" {
		os.Exit(ExecutorMain())
	}
	for _, test := range []struct {
		name, body, code string
		status           int
	}{
		{"truncated", `{"choices":[{"finish_reason":"length","message":{"content":"partial-private-output"}}]}`, "AGENT_MODEL_TRUNCATED", 200},
		{"provider_error", `{"error":{"message":"private-upstream-error endpoint=http://secret token=secret"}}`, ErrorCodeModelFailed, 200},
		{"unavailable_service", "private-upstream-503", ErrorCodeModelFailed, 503},
		{"empty", `{"choices":[{"finish_reason":"stop","message":{"content":""}}]}`, ErrorCodeEmptyResult, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			input := validProtocolInput()
			input.ModelEndpoint = server.URL
			input.ModelAPIKey = "private-api-key"
			var stdin bytes.Buffer
			if err := WriteFrame(&stdin, input); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := executorCompletionSubprocess(t, stdin.Bytes())
			if err != nil {
				t.Fatalf("safe failure exited nonzero: %v, stderr=%q", err, stderr)
			}
			var manifest ManifestFrame
			reader := bufio.NewReader(bytes.NewReader(stdout))
			if err := ReadFrame(reader, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Result.Type != "error" || manifest.Result.Text != test.code {
				t.Fatalf("manifest=%#v", manifest)
			}
			if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
				t.Fatalf("stdout not exact EOF: %v", err)
			}
			if len(stderr) != 0 {
				t.Fatalf("safe failure exposed stderr: %q", stderr)
			}
		})
	}
}

func TestExecutorMainMalformedInputRemainsNonzeroAndRedacted(t *testing.T) {
	input := validProtocolInput()
	input.Instruction = ""
	var invalid bytes.Buffer
	if err := WriteFrame(&invalid, input); err != nil {
		t.Fatal(err)
	}
	for _, stdin := range [][]byte{[]byte("private-invalid-input"), invalid.Bytes()} {
		_, stderr, err := executorCompletionSubprocess(t, stdin)
		if err == nil {
			t.Fatal("malformed input exited successfully")
		}
		if !bytes.Equal(stderr, []byte("agent executor: protocol or output failure\n")) && !bytes.Equal(stderr, []byte("agent executor: protocol or output failure\r\n")) {
			t.Fatalf("unsafe or unexpected stderr: %q", stderr)
		}
	}
}

func executorCompletionSubprocess(t *testing.T, stdin []byte) ([]byte, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExecutorMainSafeFailureUsesCleanExit$")
	command.Env = append(os.Environ(), "OPC_TEST_EXECUTOR_MAIN=1")
	command.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

type executorBrokenWriter struct{}

func (executorBrokenWriter) Write([]byte) (int, error) { return 0, errors.New("private-output-error") }

func TestExecutorFailureManifestWriteFailureIsNotSuccessful(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"length","message":{"content":"partial"}}]}`)
	}))
	defer server.Close()
	input := validProtocolInput()
	input.ModelEndpoint = server.URL
	var stdin bytes.Buffer
	if err := WriteFrame(&stdin, input); err != nil {
		t.Fatal(err)
	}
	if err := RunExecutor(&stdin, executorBrokenWriter{}, time.Second); err == nil || !strings.Contains(err.Error(), "output") {
		t.Fatalf("write failure must survive: %v", err)
	} else {
		var reported *reportedExecutorFailure
		if errors.As(err, &reported) {
			t.Fatal("failed output incorrectly marked reported")
		}
	}
}

type executorShortWriter struct{ writes, shortAt int }

func (writer *executorShortWriter) Write(data []byte) (int, error) {
	writer.writes++
	if writer.writes == writer.shortAt {
		return len(data) - 1, nil
	}
	return len(data), nil
}

func TestExecutorShortErrorFrameNeverReportsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	for _, shortAt := range []int{1, 2} {
		input := validProtocolInput()
		input.ModelEndpoint = server.URL
		var stdin bytes.Buffer
		if err := WriteFrame(&stdin, input); err != nil {
			t.Fatal(err)
		}
		err := RunExecutor(&stdin, &executorShortWriter{shortAt: shortAt}, time.Second)
		if !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("short write %d returned %v", shortAt, err)
		}
		var reported *reportedExecutorFailure
		if errors.As(err, &reported) {
			t.Fatal("short output incorrectly marked reported")
		}
	}
}

func TestExecutorResponseEnvelopeBudgetDoesNotTruncateTrailingData(t *testing.T) {
	const complete = `{"choices":[{"finish_reason":"stop","message":{"content":"not a complete envelope"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, complete+strings.Repeat(" ", (8<<20)-len(complete))+"private-trailing-data")
	}))
	defer server.Close()
	input := validProtocolInput()
	input.ModelEndpoint = server.URL
	var stdin, stdout bytes.Buffer
	if err := WriteFrame(&stdin, input); err != nil {
		t.Fatal(err)
	}
	if err := RunExecutor(&stdin, &stdout, 2*time.Second); err == nil {
		t.Fatal("oversized response was silently truncated")
	}
	var manifest ManifestFrame
	if err := ReadFrame(bufio.NewReader(&stdout), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Result.Type != "error" || manifest.Result.Text != ErrorCodeModelResponseInvalid {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestExecutorTerminalFailureAppliesToEveryFrozenOutputContract(t *testing.T) {
	for _, output := range []OutputContract{{Type: ResultTypeText}, {Type: ResultTypeFile, Name: "report.md", MIME: "text/markdown"}, {Type: ResultTypeFiles, Files: []OutputFileContract{{Name: "report.md", MIME: "text/markdown"}, {Name: "facts.json", MIME: "application/json"}}}} {
		t.Run(output.Type, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "length", "message": map[string]any{"content": `["full looking report","{}"]`}}}})
			}))
			defer server.Close()
			input := validProtocolInput()
			input.ModelEndpoint, input.OutputContract = server.URL, &output
			input.Capabilities = []string{CapabilityReadTaskSnapshot, map[string]string{ResultTypeText: CapabilityWriteTextResult, ResultTypeFile: CapabilityWriteFileResult, ResultTypeFiles: CapabilityWriteFilesResult}[output.Type]}
			var stdin, stdout bytes.Buffer
			if err := WriteFrame(&stdin, input); err != nil {
				t.Fatal(err)
			}
			if err := RunExecutor(&stdin, &stdout, time.Second); err == nil {
				t.Fatal("partial output accepted")
			}
			var manifest ManifestFrame
			if err := ReadFrame(bufio.NewReader(&stdout), &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Result.Type != "error" || manifest.Result.Text != "AGENT_MODEL_TRUNCATED" {
				t.Fatalf("manifest=%#v", manifest)
			}
		})
	}
}
