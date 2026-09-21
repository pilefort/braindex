package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const inputJSON = `{"version":1,"terms":["ai","エージェント"],"items":[{"id":"local-id","feed":"Feed","lang":"en","t":"Headline","s":"Private summary","url":"https://example.invalid/article"}]}`
const testKey = "test-secret-never-print"

func reply(w http.ResponseWriter, n float64, confidence float64) {
	fmt.Fprintf(w, `{"answers":{"worth_reading":{"noul":%v},"field":{"choice":"physics","confidence":%v}},"usage":{"input_tokens":12,"output_tokens":4}}`, n, confidence)
}
func invoke(t *testing.T, s *httptest.Server, args []string, key, input string) (int, string, string, []time.Duration) {
	t.Helper()
	var out, stderr bytes.Buffer
	var waits []time.Duration
	code := run(append([]string{"-endpoint", s.URL}, args...), strings.NewReader(input), &out, &stderr, key, s.Client(), func(d time.Duration) { waits = append(waits, d) })
	if strings.Contains(out.String()+stderr.String(), testKey) {
		t.Fatal("key leaked")
	}
	return code, out.String(), stderr.String(), waits
}
func TestPayloadAndScoring(t *testing.T) {
	for _, tc := range []struct {
		n, confidence float64
		score         int
		tag           string
		summary       bool
	}{
		{.5499, .9, 1, "physics", false}, {.55, .9, 2, "physics", false}, {.7499, .9, 2, "physics", false}, {.75, .5, 3, "physics", true}, {1, .499, 3, "", false}, {0, .9, 1, "physics", false},
	} {
		t.Run(fmt.Sprintf("%v-%v", tc.n, tc.confidence), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+testKey {
					t.Error("request headers")
				}
				var p map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
					t.Error(err)
				}
				if len(p) != 3 {
					t.Error("unexpected payload fields")
				}
				var st map[string]any
				json.Unmarshal(p["state"], &st)
				want := 3
				if tc.summary {
					want++
				}
				if len(st) != want || st["headline"] != "Headline" || st["source"] != "Feed" || st["id"] != nil || st["url"] != nil || st["lang"] != nil {
					t.Errorf("state=%v", st)
				}
				if tc.summary {
					if st["summary"] != "Private summary" {
						t.Error(st)
					}
				} else if st["summary"] != nil {
					t.Error(st)
				}
				var q map[string]question
				json.Unmarshal(p["questions"], &q)
				if !reflect.DeepEqual(q["field"].Criteria, defaultFields) || q["worth_reading"].Type != "noul" {
					t.Error(q)
				}
				reply(w, tc.n, tc.confidence)
			}))
			defer s.Close()
			args := []string{}
			if tc.summary {
				args = append(args, "-summary")
			}
			log := filepath.Join(t.TempDir(), "usage.jsonl")
			args = append(args, "-usage-log", log)
			code, out, se, _ := invoke(t, s, args, testKey, inputJSON)
			var res response
			json.Unmarshal([]byte(out), &res)
			if code != 0 || calls != 1 || len(res.Items) != 1 || res.Items[0] != (result{ID: "local-id", Score: tc.score, Tag: tc.tag}) {
				t.Fatalf("%d %s %s", code, out, se)
			}
			b, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			var usage map[string]any
			if json.Unmarshal(b, &usage) != nil || len(usage) != 5 || usage["id"] != "local-id" || usage["input_tokens"] != float64(12) || usage["output_tokens"] != float64(4) || strings.Contains(string(b), "Headline") || strings.Contains(string(b), testKey) {
				t.Fatalf("usage=%s", b)
			}
		})
	}
}
func TestRetryAndFailures(t *testing.T) {
	for _, status := range []int{429, 529, 400} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls <= 3 {
					w.WriteHeader(status)
					fmt.Fprint(w, testKey)
					return
				}
				reply(w, .8, .9)
			}))
			defer s.Close()
			code, out, se, waits := invoke(t, s, nil, testKey, inputJSON)
			if status == 400 {
				if code != 1 || calls != 1 || len(waits) != 0 {
					t.Fatalf("%d %d %v %s %s", code, calls, waits, out, se)
				}
			} else if code != 0 || calls != 4 || !reflect.DeepEqual(waits, []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}) {
				t.Fatalf("%d %d %v %s %s", code, calls, waits, out, se)
			}
		})
	}
}
func TestNoKeyAndDryRun(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; reply(w, .8, .9) }))
	defer s.Close()
	code, _, _, _ := invoke(t, s, nil, "", inputJSON)
	if code != 1 || calls != 0 {
		t.Fatalf("missing key: %d calls %d", code, calls)
	}
	code, out, se, _ := invoke(t, s, []string{"-dry-run"}, "", inputJSON)
	if code != 0 || calls != 0 || strings.TrimSpace(out) != `{"items":[]}` || !strings.Contains(se, `"headline": "Headline"`) || strings.Contains(se, "Private summary") || strings.Contains(se, "local-id") {
		t.Fatalf("%d %s %s", code, out, se)
	}
}
func TestPartialFailureAndCustomFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fields.json")
	os.WriteFile(path, []byte(`{"physics":"Custom field"}`), 0600)
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var p payload
		json.NewDecoder(r.Body).Decode(&p)
		if p.Model != "custom-model" || len(p.Questions["field"].Criteria) != 1 || p.Questions["field"].Criteria["physics"] != "Custom field" {
			t.Error(p)
		}
		if calls == 1 {
			fmt.Fprint(w, `{"answers":{}}`)
			return
		}
		reply(w, .6, .9)
	}))
	defer s.Close()
	input := `{"version":1,"terms":[],"items":[{"id":"fail"},{"id":"pass"}]}`
	code, out, se, _ := invoke(t, s, []string{"-fields", path, "-model", "custom-model", "-t2", "0.6", "-t3", "0.8"}, testKey, input)
	if code != 0 || calls != 2 || strings.TrimSpace(out) != `{"items":[{"id":"pass","r":2,"tag":"physics"}]}` {
		t.Fatalf("%d %s %s", code, out, se)
	}
}
func TestRetryLimitAndMalformedResponse(t *testing.T) {
	for _, mode := range []string{"limit", "json", "range"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch mode {
				case "limit":
					w.WriteHeader(429)
				case "json":
					fmt.Fprint(w, "broken "+testKey)
				case "range":
					reply(w, 1.1, .9)
				}
			}))
			defer s.Close()
			code, out, _, waits := invoke(t, s, nil, testKey, inputJSON)
			want := 1
			if mode == "limit" {
				want = 4
			}
			if code != 1 || calls != want || strings.TrimSpace(out) != `{"items":[]}` || len(waits) != want-1 {
				t.Fatalf("%d %d %s %v", code, calls, out, waits)
			}
		})
	}
}

func TestUsageLogIncludesFailedArticles(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	for i := 0; i < 2; i++ {
		code, _, _, _ := invoke(t, s, []string{"-usage-log", path}, testKey, inputJSON)
		if code != 1 {
			t.Fatal(code)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("%s", b)
	}
	for _, line := range lines {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) != nil || row["input_tokens"] != float64(0) || row["id"] != "local-id" {
			t.Fatal(line)
		}
	}
}
