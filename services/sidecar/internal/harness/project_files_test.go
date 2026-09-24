package harness

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

func TestProjectFilesStayPinnedDuringSelfCheckAndBudgetHandoff(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			files := []string{`{"path":"file.txt","content":"PRIVATE FILE"}`}
			request := Request{Protocol: protocol, Model: "test", ProjectFiles: files, History: []modelclient.ChatMessage{{Role: "user", Content: "analyze"}}}
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{
				{Text: `draft[opc:selfcheck]{"sufficient":false,"note":"revise"}[/opc:selfcheck]`},
				{Text: `answer[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
			}}}
			result, err := Run(context.Background(), client, request, nil, nil, Callbacks{})
			if err != nil || result.Reflections != 1 || len(client.requests) != 2 {
				t.Fatalf("run %+v %v", result, err)
			}
			for _, r := range client.requests {
				if !reflect.DeepEqual(r.ProjectFiles, files) {
					t.Fatal("one-generation file context dropped")
				}
			}
			handoff := requestForBudgetHandoff(request)
			if !reflect.DeepEqual(handoff.ProjectFiles, files) {
				t.Fatal("budget handoff dropped files")
			}
		})
	}
}

func TestProjectFilesCannotBypassPromptBudget(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			client := &handoffRecordingClient{}
			_, err := Run(context.Background(), client, Request{Protocol: protocol, Model: "test", ProjectFiles: []string{strings.Repeat("x", modelclient.MaxPromptBytes)}, History: []modelclient.ChatMessage{{Role: "user", Content: "analyze"}}}, nil, nil, Callbacks{})
			if !errors.Is(err, modelclient.ErrPromptTooLarge) || len(client.requests) != 0 {
				t.Fatalf("oversized context reached model: %v, calls=%d", err, len(client.requests))
			}
		})
	}
}
