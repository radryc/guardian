package ui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedUI(t *testing.T) {
	handler := Handler()
	cases := []struct {
		path string
		want string
	}{
		{"/", "Forge"},
		{"/app.js", "function api"},
		{"/app.css", "--accent"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest("GET", tc.path, nil))
		if recorder.Code != 200 {
			t.Fatalf("%s status = %d", tc.path, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), tc.want) {
			t.Fatalf("%s missing %q", tc.path, tc.want)
		}
	}
}
