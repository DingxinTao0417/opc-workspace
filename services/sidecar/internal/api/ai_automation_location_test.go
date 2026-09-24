package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIAutomationLocationsPreferConfirmedAttempt(t *testing.T) {
	for _, action := range []string{"automation.enable", "automation.disable", "automation.update", "automation.retry"} {
		for _, status := range []string{"pending", "rejected", "confirmed"} {
			t.Run(action+"/"+status, func(t *testing.T) {
				target, result := uuid.NewString(), uuid.NewString()
				input := aiWorkspaceAction{Action: action}
				kind := "rule"
				if action == "automation.retry" {
					kind, input.AutomationRunID = "run", target
				} else {
					input.AutomationRuleID = target
					result = target
				}
				encoded, _ := json.Marshal(input)
				now := time.Now().UTC()
				row := models.AIActionProposal{Status: status, ActionJSON: string(encoded), PreviewJSON: `{}`, CreatedAt: now.Format(time.RFC3339Nano)}
				wantID := target
				if status == "confirmed" {
					row.ResultID, wantID = &result, result
				}
				out, err := aiActionOutput(row, "completed", now)
				if err != nil || out.Route != "/settings/automation?"+kind+"="+wantID {
					t.Fatal(out.Route, err)
				}
			})
		}
	}
}
