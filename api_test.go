package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func syncRequest(t *testing.T, s *Server, head string, surveys []Survey) SyncResponse {
	t.Helper()
	body, err := json.Marshal(SyncRequest{Device: "test-dev", Head: head, Surveys: surveys})
	assertNoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/idig/Test/Trench", bytes.NewReader(body))
	req.SetBasicAuth("user", "password")
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, req)
	assertEqual(t, w.Code, http.StatusOK)

	var resp SyncResponse
	assertNoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// Two devices that are both at HEAD change a different survey each and sync at
// the same time. Only one of them can be pushed, the other one has to pull.
func TestConcurrentSync(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rootDir := t.TempDir()
	assertNoError(t, createCmd(rootDir, []string{"Test"}))
	assertNoError(t, addUserCmd(rootDir, []string{"Test", "user", "password"}))
	s := NewServer(rootDir)

	surveys := generateSurveys(300)
	head := syncRequest(t, s, "", surveys).Version

	for round := 0; round < 10; round++ {
		devices := make([][]Survey, 2)
		for i := range devices {
			devices[i] = make([]Survey, len(surveys))
			copy(devices[i], surveys)
			devices[i][i] = Survey{
				"IdentifierUUID": surveys[i]["IdentifierUUID"],
				"Title":          fmt.Sprintf("Round %d", round),
				"Type":           "Context",
			}
		}

		responses := make([]SyncResponse, len(devices))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range devices {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				responses[i] = syncRequest(t, s, head, devices[i])
			}(i)
		}
		close(start)
		wg.Wait()

		winner := 0
		if responses[0].Status != StatusPushed {
			winner = 1
		}
		loser := 1 - winner
		if responses[winner].Status != StatusPushed || responses[loser].Status != StatusPull {
			t.Fatalf("round %d: got %s and %s, want one pushed and one pull",
				round, responses[0].Status, responses[1].Status)
		}
		assertEqual(t, responses[loser].Version, responses[winner].Version)
		if len(responses[loser].Updates) != 1 {
			t.Fatalf("round %d: got %d updates, want 1", round, len(responses[loser].Updates))
		}
		assertEqual(t, responses[loser].Updates[0].Id, surveys[winner].ID())

		surveys = devices[winner]
		head = responses[winner].Version
	}
}
