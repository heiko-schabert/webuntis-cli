package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"webuntis-cli/untis"
)

// fakeUntis answers the JSON-RPC methods the tools call.
func fakeUntis(t *testing.T) *untis.Client {
	t.Helper()
	results := map[string]string{
		"getUserData2017": `{"userData":{"elemType":"LEGAL_GUARDIAN","schoolName":"Testschule","children":[{"id":7,"firstName":"Anna"},{"id":8,"firstName":"Ben"}]}}`,
		"getTimetable2017": `{"masterData":{"timeStamp":1,"subjects":[{"id":1,"name":"M","longName":"Mathe"}],
			"teachers":[{"id":1,"name":"MUE"},{"id":2,"name":"SCH"}],"rooms":[{"id":1,"name":"R101"}],"klassen":[{"id":5,"name":"5a"}]},
			"timetable":{"periods":[
			{"startDateTime":"2026-10-07T09:45Z","endDateTime":"2026-10-07T10:30Z","is":["IRREGULAR"],"text":{"substitution":"Vertretung"},
			 "elements":[{"type":"SUBJECT","id":1},{"type":"TEACHER","id":2,"orgId":1},{"type":"ROOM","id":1},{"type":"CLASS","id":5}]},
			{"startDateTime":"2026-10-07T08:00Z","endDateTime":"2026-10-07T08:45Z","is":["STANDARD"],
			 "elements":[{"type":"SUBJECT","id":1},{"type":"TEACHER","id":1}]}]}}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string           `json:"method"`
			Params []map[string]any `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Params[0]["auth"] == nil {
			t.Errorf("%s without auth", req.Method)
		}
		w.Write([]byte(`{"jsonrpc":"2.0","result":` + results[req.Method] + `}`))
	}))
	t.Cleanup(srv.Close)
	return untis.New(untis.Config{Server: srv.URL, School: "s", User: "u", Secret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"})
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), newServer(fakeUntis(t)), func() error { return nil }, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestTimetable(t *testing.T) {
	code, out, errOut := runCLI(t, "timetable", "--child", "ben")
	want := "Wed 2026-10-07\n  08:00-08:45  M                   MUE\n  09:45-10:30  M        R101       SCH  [IRREGULAR, Vertretung, teacher was MUE]\n"
	if code != 0 || !strings.Contains(out, want) {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", code, errOut, out)
	}
	_, out, _ = runCLI(t, "changes", "--json")
	if strings.Count(out, `"subject": "M"`) != 1 || !strings.Contains(out, `"original_teacher": "MUE"`) {
		t.Fatalf("changes:\n%s", out)
	}
}

func TestChildren(t *testing.T) {
	if _, out, _ := runCLI(t, "children"); out != "Anna\nBen\n" {
		t.Fatalf("stdout %q", out)
	}
}

func TestUnknownChild(t *testing.T) {
	code, _, errOut := runCLI(t, "today", "--child", "Carl")
	if code != 1 || !strings.Contains(errOut, "available: Anna, Ben") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestClassTimetable(t *testing.T) {
	if code, out, errOut := runCLI(t, "class-timetable"); code != 0 || !strings.Contains(out, "Class 5a") {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", code, errOut, out)
	}
	if code, _, errOut := runCLI(t, "class-timetable", "--class-name", "9z"); code != 1 || !strings.Contains(errOut, "available: 5a") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}
