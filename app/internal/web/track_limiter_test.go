package web

import (
	"bytes"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func TestTrackLimiterCapsPerClient(t *testing.T) {
	limiter := newTrackLimiter()
	request := httptest.NewRequest("GET", "/track/SKM-AAAA-BBBB", nil)
	request.RemoteAddr = "192.0.2.1:12345"

	for attempt := 1; attempt <= trackLimit; attempt++ {
		request.Header.Set("X-Forwarded-For", "198.51.100.1")
		if !limiter.allow(request) {
			t.Fatalf("attempt %d was rejected before the configured limit", attempt)
		}
	}
	request.Header.Set("X-Forwarded-For", "203.0.113.99")
	if limiter.allow(request) {
		t.Fatal("attempt beyond the configured limit was allowed")
	}

	otherClient := httptest.NewRequest("GET", "/track/SKM-AAAA-BBBB", nil)
	otherClient.RemoteAddr = "192.0.2.2:12345"
	if !limiter.allow(otherClient) {
		t.Fatal("an unrelated client was rate limited")
	}
}

func TestTrackingCodeIsRedactedFromRequestLogs(t *testing.T) {
	const code = "SKM-AAAA-BBBB-CCCC"
	request := httptest.NewRequest("GET", "/track/"+code, nil)
	var output bytes.Buffer
	formatter := redactingLogFormatter{
		formatter: &middleware.DefaultLogFormatter{
			Logger:  log.New(&output, "", 0),
			NoColor: true,
		},
	}
	entry := formatter.NewLogEntry(request)
	entry.Write(200, 0, nil, time.Millisecond, nil)
	if strings.Contains(output.String(), code) {
		t.Fatal("tracking code leaked into the request log")
	}
	if !strings.Contains(output.String(), "/track/{code}") {
		t.Fatalf("redacted tracking route missing from request log: %s", output.String())
	}
}
