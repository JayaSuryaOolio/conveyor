package core

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Model is a Claude model detected at runtime.
type Model struct {
	ID     string `json:"id"`
	Tier   string `json:"tier"`   // family word in the id: haiku, sonnet, opus, ...
	Latest bool   `json:"latest"` // newest id of its tier; what the tier alias resolves to
}

// ModelList is the detection result. Err is set when detection failed; then
// Models is empty and model names are accepted unchecked.
type ModelList struct {
	Models []Model   `json:"models"`
	Source string    `json:"source"` // "api" or "transcripts"
	Err    string    `json:"error,omitempty"`
	At     time.Time `json:"at"`
}

const modelsTTL = time.Hour

// Models returns the detected models, from <home>/models.json when younger
// than an hour unless refresh. Failures are not cached.
func (s *Service) Models(refresh bool) ModelList {
	path := filepath.Join(s.Home, "models.json")
	var ml ModelList
	if !refresh {
		if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &ml) == nil && time.Since(ml.At) < modelsTTL && len(ml.Models) > 0 {
			return ml
		}
	}
	ml = ModelList{At: time.Now()}
	var ids []string
	var err error
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		ml.Source = "api"
		ids, err = apiModels(cmp.Or(os.Getenv("ANTHROPIC_BASE_URL"), "https://api.anthropic.com"), key)
	} else {
		ml.Source = "transcripts"
		ids, err = transcriptModels(s.projectsDir(), 30*24*time.Hour)
	}
	if err == nil && len(ids) == 0 {
		err = fmt.Errorf("no models found")
	}
	if err != nil {
		ml.Err = err.Error()
		return ml
	}
	ml.Models = classify(ids)
	if b, err := json.Marshal(ml); err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
	return ml
}

// apiModels lists model ids from the Anthropic Models API.
func apiModels(base, key string) ([]string, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(base, "/")+"/v1/models?limit=1000", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("models API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models API: %s", resp.Status)
	}
	return parseAPIModels(resp.Body)
}

func parseAPIModels(r io.Reader) ([]string, error) {
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return nil, fmt.Errorf("models API: %w", err)
	}
	var ids []string
	for _, d := range body.Data {
		ids = append(ids, d.ID)
	}
	return ids, nil
}

var transcriptModelRE = regexp.MustCompile(`"model":"(claude-[a-z0-9.-]+)"`)

// transcriptModels returns the model ids Claude Code sessions actually ran
// with, from the tail of each transcript modified within maxAge.
// ponytail: reads the last 64KB per file to stay fast on GBs of transcripts;
// a model used only early in a long session is missed.
func transcriptModels(dir string, maxAge time.Duration) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	buf := make([]byte, 64<<10)
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil || time.Since(fi.ModTime()) > maxAge {
			continue
		}
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		n, _ := fh.ReadAt(buf, max(0, fi.Size()-int64(len(buf))))
		fh.Close()
		for _, m := range transcriptModelRE.FindAllSubmatch(buf[:n], -1) {
			seen[string(m[1])] = true
		}
	}
	var ids []string
	for id := range seen {
		ids = append(ids, id)
	}
	return ids, nil
}

// modelTier is the first non-numeric segment after "claude-": claude-opus-5-5
// and claude-3-5-sonnet-20241022 give opus and sonnet.
func modelTier(id string) string {
	for _, p := range strings.Split(strings.TrimPrefix(id, "claude-"), "-") {
		if _, err := strconv.Atoi(p); err != nil {
			return p
		}
	}
	return ""
}

// versionKey orders ids by their version numbers, then by date suffix.
func versionKey(id string) []int {
	var v []int
	date := 0
	for _, p := range strings.Split(id, "-") {
		if n, err := strconv.Atoi(p); err == nil {
			if len(p) == 8 {
				date = n
			} else {
				v = append(v, n)
			}
		}
	}
	return append(v, date)
}

// classify sorts ids by tier then newest first and marks the latest per tier.
func classify(ids []string) []Model {
	var ms []Model
	for _, id := range ids {
		if t := modelTier(id); t != "" {
			ms = append(ms, Model{ID: id, Tier: t})
		}
	}
	slices.SortFunc(ms, func(a, b Model) int {
		return cmp.Or(cmp.Compare(a.Tier, b.Tier), slices.Compare(versionKey(b.ID), versionKey(a.ID)), cmp.Compare(a.ID, b.ID))
	})
	for i := range ms {
		ms[i].Latest = i == 0 || ms[i-1].Tier != ms[i].Tier
	}
	return ms
}

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

// Resolve maps a tier alias to its latest id; ids (with an optional [1m]
// suffix, or without their -YYYYMMDD date) resolve to the detected id.
// "" when m is unknown or detection failed.
func (ml ModelList) Resolve(m string) string {
	base := strings.TrimSuffix(m, "[1m]")
	for _, x := range ml.Models {
		if x.ID == base || dateSuffix.ReplaceAllString(x.ID, "") == base || (x.Latest && x.Tier == base) {
			return x.ID
		}
	}
	return ""
}

// Check validates a model name. "" (inherit the default) and "inherit" always
// pass. When detection failed it returns a warning instead of an error.
func (ml ModelList) Check(m string) (warning string, err error) {
	if m == "" || m == "inherit" {
		return "", nil
	}
	if len(ml.Models) == 0 {
		return fmt.Sprintf("model detection failed (%s); %q not checked", ml.Err, m), nil
	}
	if ml.Resolve(m) != "" {
		return "", nil
	}
	var names []string
	for _, x := range ml.Models {
		if x.Latest {
			names = append(names, x.Tier)
		}
		names = append(names, x.ID)
	}
	best := slices.MinFunc(names, func(a, b string) int { return cmp.Compare(levenshtein(m, a), levenshtein(m, b)) })
	return "", fmt.Errorf("unknown model %q; did you mean %q? (see conveyor models; --force to set it anyway)", m, best)
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
