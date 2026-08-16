//go:build integration

package nlp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/domain/nlp"
	"github.com/nicoflow/nicoflow-api/internal/handler"
	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

const integrationJWTSecret = "integration-test-secret-32-bytes!!"

type apiEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// newNLPServer spins up a real httptest.Server with only the NLP handler
// wired — the route is stateless (no DB), so no pool/testDB is needed.
func newNLPServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	cfg := config.Config{
		JWTSecret:          integrationJWTSecret,
		JWTExpiry:          15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}
	h := handler.Handlers{
		NLP: nlp.NewHandler(nlp.NewService()),
	}
	srv := httptest.NewServer(handler.New(cfg, nil, h))
	t.Cleanup(srv.Close)

	token, err := jwtutil.Issue("test-user-id", "nlp-integration@test.dev", "free", integrationJWTSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("issue jwt: %v", err)
	}
	return srv, token
}

func postParseDate(t *testing.T, srv *httptest.Server, token string, req nlp.ParseDateRequest) (*http.Response, apiEnvelope) {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	httpReq, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/nlp/parse-date", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	var env apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp, env
}

func TestParseDate_English_NextFriday(t *testing.T) {
	srv, token := newNLPServer(t)

	now := time.Now().UTC()
	resp, env := postParseDate(t, srv, token, nlp.ParseDateRequest{
		Text:     "next friday",
		Timezone: "UTC",
		Locale:   "en",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, env.Data)
	}

	var out nlp.ParseDateResponse
	if err := json.Unmarshal(env.Data, &out); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if out.Confidence != "high" || out.Date == nil {
		t.Fatalf("got confidence=%q date=%v, want high confidence with a resolved date", out.Confidence, out.Date)
	}

	resolved, err := time.Parse("2006-01-02", *out.Date)
	if err != nil {
		t.Fatalf("resolved date %q not parseable: %v", *out.Date, err)
	}
	if resolved.Weekday() != time.Friday {
		t.Fatalf("resolved date %q is a %s, want Friday", *out.Date, resolved.Weekday())
	}
	if resolved.Before(now) {
		t.Fatalf("resolved date %q is before reference now %q", *out.Date, now)
	}
}

func TestParseDate_Russian_NextFriday(t *testing.T) {
	srv, token := newNLPServer(t)

	resp, env := postParseDate(t, srv, token, nlp.ParseDateRequest{
		Text:     "в следующую пятницу",
		Timezone: "UTC",
		Locale:   "ru",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, env.Data)
	}

	var out nlp.ParseDateResponse
	if err := json.Unmarshal(env.Data, &out); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if out.Confidence != "high" || out.Date == nil {
		t.Fatalf("got confidence=%q date=%v, want high confidence with a resolved date", out.Confidence, out.Date)
	}

	resolved, err := time.Parse("2006-01-02", *out.Date)
	if err != nil {
		t.Fatalf("resolved date %q not parseable: %v", *out.Date, err)
	}
	if resolved.Weekday() != time.Friday {
		t.Fatalf("resolved date %q is a %s, want Friday", *out.Date, resolved.Weekday())
	}
}

func TestParseDate_InvalidLocale_Rejected(t *testing.T) {
	srv, token := newNLPServer(t)

	resp, env := postParseDate(t, srv, token, nlp.ParseDateRequest{
		Text:     "next friday",
		Timezone: "UTC",
		Locale:   "he",
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	if env.Error == nil || env.Error.Code != "INVALID_INPUT" {
		t.Fatalf("error = %+v, want INVALID_INPUT", env.Error)
	}
}

func TestParseDate_NoAuth_Rejected(t *testing.T) {
	srv, _ := newNLPServer(t)

	body, _ := json.Marshal(nlp.ParseDateRequest{Text: "next friday", Timezone: "UTC", Locale: "en"})
	resp, err := http.Post(srv.URL+"/v1/nlp/parse-date", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
