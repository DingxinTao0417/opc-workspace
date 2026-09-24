package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func assertAIClientRecordResponseRoute(t *testing.T, body []byte, clientID, kind string) {
	t.Helper()
	var response struct {
		Data aiActionResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	out := response.Data
	_, id := aiActionTarget(out.Action)
	if out.ResultID != nil {
		id = *out.ResultID
	}
	want := "/clients/" + clientID
	if id != "" {
		want += "?" + kind + "=" + id
	}
	if out.Route != want {
		t.Fatalf("wrong record location: got %q, want %q", out.Route, want)
	}
}

func TestAIClientRecordLocationsPreferConfirmedResultAndKeepPendingIdentity(t *testing.T) {
	clientID, targetID, resultID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, action := range []string{"client_activity.create", "client_activity.update", "client_activity.delete", "client_followup.create", "client_followup.update", "client_followup.cancel", "client_followup.complete", "client_followup.skip", "client_followup.reschedule"} {
		for _, confirmed := range []bool{false, true} {
			t.Run(action+map[bool]string{true: "/confirmed", false: "/pending"}[confirmed], func(t *testing.T) {
				kind := strings.Split(strings.TrimPrefix(action, "client_"), ".")[0]
				input := aiWorkspaceAction{Action: action}
				id := ""
				if !strings.HasSuffix(action, ".create") {
					id = targetID
				}
				if kind == "activity" {
					input.ClientActivityID = id
				} else {
					input.ClientFollowupID = id
				}
				encoded, _ := json.Marshal(input)
				preview, _ := json.Marshal(map[string]any{"after": map[string]any{"client_id": clientID}})
				now := time.Now().UTC()
				row := models.AIActionProposal{ActionJSON: string(encoded), PreviewJSON: string(preview), Status: "pending", CreatedAt: now.Format(time.RFC3339Nano)}
				if confirmed {
					row.Status = "confirmed"
					row.ResultID = &resultID
					id = resultID
				}
				out, err := aiActionOutput(row, "completed", now)
				if err != nil {
					t.Fatal(err)
				}
				want := "/clients/" + clientID
				if id != "" {
					want += "?" + kind + "=" + id
				}
				if out.Route != want {
					t.Fatalf("got %q, want %q", out.Route, want)
				}
			})
		}
	}
}
