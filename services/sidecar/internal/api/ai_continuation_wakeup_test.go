package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func assertAIContinuationWake(t *testing.T, wake <-chan struct{}, want bool) {
	t.Helper()
	select {
	case <-wake:
		if !want {
			t.Fatal("continuation was woken without a newly committed fact")
		}
	default:
		if want {
			t.Fatal("newly committed fact did not wake continuation")
		}
	}
}

func TestAIContinuationWakeFansOutWithoutConsumingCoordinatorScan(t *testing.T) {
	coordinatorScan := make(chan struct{}, 1)
	coordinator := &aiContinuationCoordinator{wakeup: coordinatorScan}
	first, unsubscribeFirst := coordinator.subscribeFactWake()
	second, unsubscribeSecond := coordinator.subscribeFactWake()
	defer unsubscribeFirst()
	defer unsubscribeSecond()

	coordinator.wake()
	assertAIContinuationWake(t, first, true)
	assertAIContinuationWake(t, second, true)
	assertAIContinuationWake(t, coordinatorScan, true)

	unsubscribeFirst()
	coordinator.wake()
	assertAIContinuationWake(t, first, false)
	assertAIContinuationWake(t, second, true)
	assertAIContinuationWake(t, coordinatorScan, true)
}

func TestAIContinuationWakeOnlyAfterNewActionDecisionCommit(t *testing.T) {
	_, store, service, tool, generation := aiActionTestFixture(t)
	row := proposeTestAction(t, store, tool, `{"action":"task.create","changes":{"title":"Wake after commit"}}`)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}

	wake := make(chan struct{}, 1)
	service.aiContinuations = &aiContinuationCoordinator{wakeup: wake}
	router := gin.New()
	router.POST("/api/v1/ai/actions/:id/decision", service.decideAIWorkspaceAction)
	path := "/api/v1/ai/actions/" + row.ID + "/decision"

	failed := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(
		`{"fingerprint":%q,"decision":"confirm"}`, strings.Repeat("0", 64),
	)), nil)
	if failed.Code != http.StatusConflict {
		t.Fatalf("failed decision=%d %s", failed.Code, failed.Body.String())
	}
	assertAIContinuationWake(t, wake, false)

	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	confirmed := performRequest(router, http.MethodPost, path, body, nil)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("new decision=%d %s", confirmed.Code, confirmed.Body.String())
	}
	assertAIContinuationWake(t, wake, true)

	replayed := performRequest(router, http.MethodPost, path, body, nil)
	if replayed.Code != http.StatusOK {
		t.Fatalf("decision replay=%d %s", replayed.Code, replayed.Body.String())
	}
	assertAIContinuationWake(t, wake, false)
}
