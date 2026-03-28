package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"nicoflow-api/internal/response"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupRouter(handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/test", handler)
	return r
}

func TestResponseSuccess(t *testing.T) {
	tests := []struct {
		name       string
		data       any
		wantStatus int
	}{
		{
			name:       "return success",
			data:       gin.H{"id": "123"},
			wantStatus: 200,
		},
		{
			name:       "return success with empty data",
			data:       gin.H{},
			wantStatus: 200,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := setupRouter(func(ctx *gin.Context) {
				response.RespondOK(ctx, tc.data)
			})
			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			r.ServeHTTP(w, req)

			// check status code
			if w.Code != tc.wantStatus {
				t.Errorf("expected status code %d, got %d", tc.wantStatus, w.Code)
			}

			// decode response body
			var responseBody struct {
				Data  any `json:"data"`
				Error any `json:"error"`
			}
			json.NewDecoder(w.Body).Decode(&responseBody)

			// check response body
			if responseBody.Data == nil {
				t.Errorf("expected data to be non-nil, got nil")
			}

			if responseBody.Error != nil {
				t.Errorf("expected error to be nil, got %v", responseBody.Error)
			}

		})
	}
}

func TestResponseError(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		code        string
		message     string
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "return error 400",
			status:      http.StatusBadRequest,
			code:        response.ErrInvalidInput,
			message:     "invalid input",
			wantStatus:  400,
			wantCode:    response.ErrInvalidInput,
			wantMessage: "invalid input",
		},
		{
			name:        "return error 503",
			status:      http.StatusServiceUnavailable,
			code:        response.ErrServiceUnavailable,
			message:     "service unavailable",
			wantStatus:  503,
			wantCode:    response.ErrServiceUnavailable,
			wantMessage: "service unavailable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := setupRouter(func(ctx *gin.Context) {
				response.RespondError(ctx, tc.status, tc.code, tc.message)
			})
			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			r.ServeHTTP(w, req)

			// decode response body
			var responseBody struct {
				Data  any `json:"data"`
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			json.NewDecoder(w.Body).Decode(&responseBody)

			// check code
			if responseBody.Error.Code != tc.wantCode {
				t.Errorf("expected code %s, got %s", tc.wantCode, responseBody.Error.Code)
			}

			// check status
			if w.Code != tc.wantStatus {
				t.Errorf("expected status code %d, got %d", tc.wantStatus, w.Code)
			}

			// check data
			if responseBody.Data != nil {
				t.Errorf("expected data to be nil, got %v", responseBody.Data)
			}

			// check message
			if responseBody.Error.Message != tc.wantMessage {
				t.Errorf("expected message %s, got %s", tc.wantMessage, responseBody.Error.Message)
			}

		})
	}
}

func TestRespondCreate(t *testing.T) {
	tests := []struct {
		name       string
		data       any
		wantStatus int
	}{
		{
			name: "return created",
			data: gin.H{
				"id":   "123",
				"name": "test",
			},
			wantStatus: 201,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := setupRouter(func(ctx *gin.Context) {
				response.RespondCreated(ctx, tc.data)
			})
			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			r.ServeHTTP(w, req)

			// check status
			if w.Code != tc.wantStatus {
				t.Errorf("expected status code %d, got %d", tc.wantStatus, w.Code)
			}

			// decode response body
			var responseBody struct {
				Data  any `json:"data"`
				Error any `json:"error"`
			}
			json.NewDecoder(w.Body).Decode(&responseBody)

			// check response body
			if responseBody.Data == nil {
				t.Errorf("expected data to be non-nil, got nil")
			}

			if responseBody.Error != nil {
				t.Errorf("expected error to be nil, got %v", responseBody.Error)
			}
		})
	}
}
