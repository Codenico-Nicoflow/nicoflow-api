package expopush

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &client{http: srv.Client(), url: srv.URL}
}

func TestSend_MapsTicketsToTokens(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[
			{"status":"ok","id":"r1"},
			{"status":"error","message":"gone","details":{"error":"DeviceNotRegistered"}}
		]}`)
	})

	results, err := c.Send(context.Background(), []Message{{To: "t1"}, {To: "t2"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].Token != "t1" || results[0].Unregistered || results[0].Err != nil {
		t.Fatalf("first result %+v, want a clean ok for t1", results[0])
	}
	// Order matters: a mis-mapped ticket would prune the wrong device.
	if results[1].Token != "t2" || !results[1].Unregistered {
		t.Fatalf("second result %+v, want t2 flagged unregistered", results[1])
	}
}

func TestSend_NonDeviceErrorIsNotPruned(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"status":"error","message":"too big","details":{"error":"MessageTooBig"}}]}`)
	})

	results, err := c.Send(context.Background(), []Message{{To: "t1"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Only DeviceNotRegistered means the token is dead. Pruning on any error
	// would drop live devices over a transient or payload problem.
	if results[0].Unregistered {
		t.Fatal("MessageTooBig must not mark the token unregistered")
	}
	if results[0].Err == nil {
		t.Fatal("want the per-message error reported")
	}
}

// A short ticket array would silently shift results onto the wrong tokens, so it
// is treated as a failed batch rather than trusted.
func TestSend_MismatchedTicketCountFails(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"status":"ok","id":"r1"}]}`)
	})

	if _, err := c.Send(context.Background(), []Message{{To: "t1"}, {To: "t2"}}); err == nil {
		t.Fatal("want an error when tickets and messages do not line up")
	}
}

func TestSend_RequestLevelErrorFails(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errors":[{"code":"PUSH_TOO_MANY_EXPERIENCE_IDS","message":"nope"}]}`)
	})

	if _, err := c.Send(context.Background(), []Message{{To: "t1"}}); err == nil {
		t.Fatal("want an error when Expo reports a request-level failure")
	}
}

func TestSend_NonOKStatusFails(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	if _, err := c.Send(context.Background(), []Message{{To: "t1"}}); err == nil {
		t.Fatal("want an error on a non-200 response")
	}
}

func TestSend_PostsTheMessagesAsAnArray(t *testing.T) {
	var got []Message
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"data":[{"status":"ok","id":"r1"}]}`)
	})

	if _, err := c.Send(context.Background(), []Message{{To: "t1", Title: "T", Body: "B"}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(got) != 1 || got[0].To != "t1" || got[0].Title != "T" || got[0].Body != "B" {
		t.Fatalf("posted %+v, want the message intact", got)
	}
}

func TestSend_RejectsOversizedBatch(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("an oversized batch must never reach the network")
	})

	msgs := make([]Message, MaxBatchSize+1)
	if _, err := c.Send(context.Background(), msgs); err == nil {
		t.Fatal("want an error for a batch over the documented limit")
	}
}

func TestNoop_ReportsSuccessWithoutPruning(t *testing.T) {
	results, err := New(false, "").Send(context.Background(), []Message{{To: "t1"}, {To: "t2"}})
	if err != nil {
		t.Fatalf("noop Send: %v", err)
	}
	// A disabled transport must never look like a dead device, or every token
	// would be pruned on a misconfigured environment.
	for _, res := range results {
		if res.Unregistered || res.Err != nil {
			t.Fatalf("noop result %+v, want a clean success", res)
		}
	}
}

func TestSend_EmptyBatchIsANoOp(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("an empty batch must never reach the network")
	})

	results, err := c.Send(context.Background(), nil)
	if err != nil || results != nil {
		t.Fatalf("want a silent no-op, got %v / %v", results, err)
	}
}
