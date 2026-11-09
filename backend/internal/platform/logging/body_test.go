package logging

import (
	"strings"
	"testing"
)

// The case this whole file exists for. Before bodies were logged at all, the
// reason no password reached a log was that no body did; once one does, the
// sign-in body is the first thing that would.
func TestBodyRedactsTheSignInCredentials(t *testing.T) {
	rendered := Body("application/json",
		[]byte(`{"username":"someone","password":"hunter2","totp":"123456"}`))

	if strings.Contains(rendered, "hunter2") {
		t.Errorf("the password reached the log line: %s", rendered)
	}
	if strings.Contains(rendered, "123456") {
		t.Errorf("the one-time code reached the log line: %s", rendered)
	}
	if !strings.Contains(rendered, "someone") {
		t.Errorf("the username was removed, which is the part worth keeping: %s", rendered)
	}
}

func TestBodyRedactsBySeveralNames(t *testing.T) {
	for name, body := range map[string]string{
		"password":       `{"password":"x"}`,
		"mixed case":     `{"Password":"x"}`,
		"prefixed":       `{"adminPassword":"x"}`,
		"secret":         `{"sessionSecret":"x"}`,
		"token":          `{"releaseToken":"x"}`,
		"api key":        `{"api_key":"x"}`,
		"authorization":  `{"Authorization":"Bearer x"}`,
		"nested":         `{"outer":{"password":"x"}}`,
		"inside a list":  `{"accounts":[{"password":"x"}]}`,
		"a whole object": `{"credentials":{"user":"a","pass":"x"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if rendered := Body("application/json", []byte(body)); strings.Contains(rendered, `"x"`) {
				t.Errorf("a secret survived: %s", rendered)
			}
		})
	}
}

// A body that is not JSON is not rendered at all. Guessing at one would be the
// single case where something nobody listed reaches a log.
func TestBodyKeepsOutWhatItCannotRead(t *testing.T) {
	for name, tc := range map[string]struct{ contentType, body string }{
		"not json at all":   {"text/plain", "password=hunter2"},
		"json content type": {"application/json", "this is not json"},
		"empty":             {"application/json", ""},
		"a certificate":     {"application/octet-stream", "-----BEGIN PRIVATE KEY-----"},
	} {
		t.Run(name, func(t *testing.T) {
			if rendered := Body(tc.contentType, []byte(tc.body)); rendered != "" {
				t.Errorf("rendered a body it cannot read: %q", rendered)
			}
		})
	}
}

// A result set is what a response body is here, and the log is not where one
// belongs. Truncation says so rather than silently keeping the first part.
func TestBodyTruncatesAResultSet(t *testing.T) {
	big := `{"rows":"` + strings.Repeat("a", MaxBodyBytes*2) + `"}`

	rendered := Body("application/json", []byte(big))
	if len(rendered) > MaxBodyBytes+len("…[truncated]") {
		t.Errorf("length = %d, want it capped near %d", len(rendered), MaxBodyBytes)
	}
	if !strings.HasSuffix(rendered, "[truncated]") {
		t.Error("a truncated body does not say it was truncated")
	}
}

// Two log lines for the same call should be comparable, which they are not if
// the map's iteration order decides the field order.
func TestBodyRendersTheSameBodyTheSameWay(t *testing.T) {
	body := []byte(`{"b":1,"a":2,"c":{"e":3,"d":4}}`)

	first := Body("application/json", body)
	for range 20 {
		if again := Body("application/json", body); again != first {
			t.Fatalf("two renderings differ:\n%s\n%s", first, again)
		}
	}
}
