package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The app sends up to 60 000 characters of crash report and keeps it until the server
// accepts it. Only 16 KB of the request was read, so a longer report was cut mid-JSON,
// refused as invalid, and sent again at every start, never logged.
func TestCrashReportOfFullSizeIsAccepted(t *testing.T) {
	s := &Server{}
	for _, chars := range []int{8_000, 17_000, 60_000} {
		line := "\tat tv.roxy.tv.ui.Foo.bar(Foo.kt:12)\n"
		body, _ := json.Marshal(map[string]string{"report": strings.Repeat(line, chars/len(line))})
		rec := httptest.NewRecorder()
		s.clientCrash(rec, httptest.NewRequest(http.MethodPost, "/v1/diagnostics/crash", strings.NewReader(string(body))))
		if rec.Code != http.StatusNoContent {
			t.Errorf("report of %d characters (%d bytes of JSON): %d %s, want 204", chars, len(body), rec.Code, rec.Body)
		}
	}
	// Still bounded: a body past the limit is refused, not read whole.
	huge, _ := json.Marshal(map[string]string{"report": strings.Repeat("x", maxCrashBody+1)})
	rec := httptest.NewRecorder()
	s.clientCrash(rec, httptest.NewRequest(http.MethodPost, "/v1/diagnostics/crash", strings.NewReader(string(huge))))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("report past the limit: %d, want 400", rec.Code)
	}
}
