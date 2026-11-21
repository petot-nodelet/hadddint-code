package web

import (
	"net/http/httptest"
	"testing"
)

func TestTrackLimiterCapsPerClient(t *testing.T) {
	limiter := newTrackLimiter()
	request := httptest.NewRequest("GET", "/track/SKM-AAAA-BBBB", nil)
	request.RemoteAddr = "192.0.2.1:12345"

	for attempt := 1; attempt <= trackLimit; attempt++ {
		if !limiter.allow(request) {
			t.Fatalf("attempt %d was rejected before the configured limit", attempt)
		}
	}
	if limiter.allow(request) {
		t.Fatal("attempt beyond the configured limit was allowed")
	}

	otherClient := httptest.NewRequest("GET", "/track/SKM-AAAA-BBBB", nil)
	otherClient.RemoteAddr = "192.0.2.2:12345"
	if !limiter.allow(otherClient) {
		t.Fatal("an unrelated client was rate limited")
	}
}
