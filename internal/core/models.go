package core

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Model is a Claude model.
type Model struct {
	ID     string `json:"id"`
	Tier   string `json:"tier"`   // family word in the id: haiku, sonnet, opus, ...
	Latest bool   `json:"latest"` // newest id of its tier; what the tier alias resolves to
}

// ModelList is the model catalogue. Source is "api" or "builtin".
type ModelList struct {
	Models []Model `json:"models"`
	Source string  `json:"source"`
}

// builtinModels is the fallback when no ANTHROPIC_API_KEY is set (or the API
// call fails), taken from the user's own account.
// ponytail: static list, refresh it when new models ship; set ANTHROPIC_API_KEY to detect instead.
var builtinModels = []string{"claude-sonnet-5-5", "claude-opus-5-5", "claude-fable-5-1", "claude-haiku-5-5"}

// Models lists the Anthropic Models API when ANTHROPIC_API_KEY is set, else (or if that fails) builtinModels.
// Nothing is validated against it: a wrong model makes the session fail and the task go to needs_you.
func (s *Service) Models() ModelList {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		if ids, err := apiModels(cmp.Or(os.Getenv("ANTHROPIC_BASE_URL"), "https://api.anthropic.com"), key); err == nil && len(ids) > 0 {
			return ModelList{Models: classify(ids), Source: "api"}
		}
	}
	return ModelList{Models: classify(builtinModels), Source: "builtin"}
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
		if t := modelTier(id); t != "" && strings.HasPrefix(id, "claude-") {
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
