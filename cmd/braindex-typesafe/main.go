// braindex-typesafe は明示的に導入した利用者だけが使う外部採点プログラム。
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"time"
)

type item struct {
	ID      string `json:"id"`
	Feed    string `json:"feed"`
	Lang    string `json:"lang"`
	Title   string `json:"t"`
	Summary string `json:"s"`
}
type request struct {
	Version int      `json:"version"`
	Terms   []string `json:"terms"`
	Items   []item   `json:"items"`
}
type result struct {
	ID    string `json:"id"`
	Score int    `json:"r"`
	Tag   string `json:"tag,omitempty"`
}
type response struct {
	Items []result `json:"items"`
}
type question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}
type state struct {
	Headline  string   `json:"headline"`
	Source    string   `json:"source"`
	Interests []string `json:"reader_interests"`
	Summary   *string  `json:"summary,omitempty"`
}
type payload struct {
	Model     string              `json:"model"`
	State     state               `json:"state"`
	Questions map[string]question `json:"questions"`
}
type answer struct {
	Answers struct {
		Worth struct {
			Noul *float64 `json:"noul"`
		} `json:"worth_reading"`
		Field struct {
			Choice     string   `json:"choice"`
			Confidence *float64 `json:"confidence"`
		} `json:"field"`
	} `json:"answers"`
	Usage struct {
		Input  int `json:"input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
}

var defaultFields = map[string]string{
	"it":            "Software, programming, developer tools, AI/ML, cloud, security, the tech industry",
	"chemistry":     "Chemistry, materials, chemical biology, chemical engineering",
	"physics":       "Physics, astronomy, quantum technology",
	"metrology":     "Measurement standards, SI units, precision measurement",
	"other_science": "Science or engineering outside the fields above",
	"other":         "None of the above",
}

func body(it item, terms []string, model string, summary bool, fields map[string]string) payload {
	if terms == nil {
		terms = []string{}
	}
	p := payload{Model: model, State: state{Headline: it.Title, Source: it.Feed, Interests: terms}, Questions: map[string]question{
		"worth_reading": {Type: "noul", Instructions: "Would a reader whose interests are listed in `reader_interests` find the article behind `headline` worth reading?", Criteria: map[string]string{
			"true":  "The topic matches the listed interests and the headline suggests substantive content",
			"false": "Off-topic for those interests, or the headline suggests promotion, gossip, or thin content",
		}},
		"field": {Type: "choice", Instructions: "Which field does the article behind `headline` belong to?", Criteria: fields},
	}}
	if summary {
		p.State.Summary = &it.Summary
	}
	return p
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv("TYPESAFE_API_KEY"), &http.Client{Timeout: 30 * time.Second}, time.Sleep))
}

// run の HTTP クライアントと待機をテストで差し替え、外部通信と実時間の待機を避ける。
func run(args []string, in io.Reader, out, errout io.Writer, key string, client *http.Client, sleep func(time.Duration)) int {
	fs := flag.NewFlagSet("braindex-typesafe", flag.ContinueOnError)
	fs.SetOutput(errout)
	endpoint := fs.String("endpoint", "https://api.typesafe.ai/v1/systemone", "送信先 URL")
	model := fs.String("model", "jev-latest", "モデル名")
	summary := fs.Bool("summary", false, "概要も送る")
	dry := fs.Bool("dry-run", false, "送信せず本文を標準エラーに出す")
	fieldsPath := fs.String("fields", "", "分野名から説明への JSON ファイル")
	usagePath := fs.String("usage-log", "", "利用量を追記する JSONL ファイル")
	t2 := fs.Float64("t2", .55, "2 点の下限")
	t3 := fs.Float64("t3", .75, "3 点の下限")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	fail := func(msg string) int { fmt.Fprintln(errout, "braindex-typesafe:", msg); return 1 }
	if fs.NArg() != 0 {
		return fail("位置引数は受け付けません")
	}
	if math.IsNaN(*t2) || math.IsNaN(*t3) || *t2 < 0 || *t2 > *t3 || *t3 > 1 {
		return fail("しきい値は 0 <= t2 <= t3 <= 1")
	}
	if key == "" && !*dry {
		return fail("TYPESAFE_API_KEY がありません")
	}
	fields := defaultFields
	if *fieldsPath != "" {
		b, err := os.ReadFile(*fieldsPath)
		if err != nil {
			return fail("分野ファイルを読めません")
		}
		fields = nil
		if json.Unmarshal(b, &fields) != nil || len(fields) == 0 || len(fields) > 255 {
			return fail("分野は名前と説明の対応を 1〜255 件指定してください")
		}
		for k, v := range fields {
			if k == "" || v == "" {
				return fail("分野の名前と説明は空にできません")
			}
		}
	}
	var req request
	dec := json.NewDecoder(in)
	if dec.Decode(&req) != nil || req.Version != 1 {
		return fail("version 1 の依頼 JSON が必要です")
	}
	var trailing any
	if dec.Decode(&trailing) != io.EOF {
		return fail("依頼は JSON 1 件にしてください")
	}
	var logFile *os.File
	if *usagePath != "" && !*dry {
		var err error
		logFile, err = os.OpenFile(*usagePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return fail("利用量ログを開けません")
		}
		defer logFile.Close()
	}
	res := response{Items: []result{}}
	for _, it := range req.Items {
		p := body(it, req.Terms, *model, *summary, fields)
		if *dry {
			enc := json.NewEncoder(errout)
			enc.SetIndent("", "  ")
			if enc.Encode(p) != nil {
				return 1
			}
			continue
		}
		b, _ := json.Marshal(p)
		start := time.Now()
		a, err := call(client, *endpoint, key, b, sleep)
		if logFile != nil {
			record := struct {
				Time   string `json:"time"`
				ID     string `json:"id"`
				Input  int    `json:"input_tokens"`
				Output int    `json:"output_tokens"`
				MS     int64  `json:"ms"`
			}{time.Now().UTC().Format(time.RFC3339Nano), it.ID, a.Usage.Input, a.Usage.Output, time.Since(start).Milliseconds()}
			if json.NewEncoder(logFile).Encode(record) != nil {
				return fail("利用量ログを書けません")
			}
		}
		if err != nil {
			// 応答本文・URL・HTTP エラーにはキーが含まれうるため表示しない。
			fmt.Fprintln(errout, "braindex-typesafe: 記事の判定に失敗しました")
			continue
		}
		n := a.Answers.Worth.Noul
		if n == nil || *n < 0 || *n > 1 {
			fmt.Fprintln(errout, "braindex-typesafe: 確率が不正です")
			continue
		}
		score := 1
		if *n >= *t3 {
			score = 3
		} else if *n >= *t2 {
			score = 2
		}
		r := result{ID: it.ID, Score: score}
		f := a.Answers.Field
		if _, ok := fields[f.Choice]; ok && f.Confidence != nil && *f.Confidence >= .5 && *f.Confidence <= 1 {
			r.Tag = f.Choice
		}
		res.Items = append(res.Items, r)
	}
	if json.NewEncoder(out).Encode(res) != nil {
		return fail("応答を書けません")
	}
	if !*dry && len(req.Items) > 0 && len(res.Items) == 0 {
		return 1
	}
	return 0
}

func call(client *http.Client, endpoint, key string, data []byte, sleep func(time.Duration)) (answer, error) {
	// リダイレクト先へ認証情報や本文を渡さない。
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
		if err != nil {
			return answer{}, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return answer{}, err
		}
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if (resp.StatusCode == 429 || resp.StatusCode == 529) && attempt < 3 {
			sleep(time.Duration(2<<attempt) * time.Second)
			continue
		}
		if readErr != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return answer{}, errors.New("HTTP failure")
		}
		var a answer
		if err := json.Unmarshal(b, &a); err != nil {
			return answer{}, err
		}
		return a, nil
	}
}
