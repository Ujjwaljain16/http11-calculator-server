package httpmsg_test

import (
	"testing"

	"calcserver/internal/httpmsg"
)

func TestStatus_ReasonPhrase(t *testing.T) {
	cases := map[httpmsg.Status]string{
		httpmsg.StatusOK:               "OK",
		httpmsg.StatusBadRequest:       "Bad Request",
		httpmsg.StatusNotFound:         "Not Found",
		httpmsg.StatusMethodNotAllowed: "Method Not Allowed",
	}
	for status, want := range cases {
		if got := status.ReasonPhrase(); got != want {
			t.Errorf("Status(%d).ReasonPhrase() = %q, want %q", status, got, want)
		}
	}
}
