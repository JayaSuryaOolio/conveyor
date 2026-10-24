// Package statusblock parses the <conveyor> block an agent ends its run with (tech-spec §9).
package statusblock

import (
	"regexp"
	"strings"
)

// Criterion is one acceptance-criteria checklist line.
type Criterion struct {
	Met  bool
	Text string
}

// Block is a parsed status block.
type Block struct {
	Status               string // "done" | "failed"
	Summary, State, Next string
	Criteria             []Criterion
	Learnings, Spawn     []string
}

var (
	blockRe = regexp.MustCompile(`(?is)<conveyor>(.*?)</conveyor>`)
	keyRe   = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z_-]*)\s*:\s*(.*)$`)
	critRe  = regexp.MustCompile(`^\s*[-*]\s*\[([ xX])\]\s*(.*)$`)
)

// Parse returns the last valid block and the message with that block removed (trimmed).
// ok=false when there is no valid block (missing, or bad status); rest is then msg unchanged.
func Parse(msg string) (b Block, rest string, ok bool) {
	locs := blockRe.FindAllStringSubmatchIndex(msg, -1)
	for i := len(locs) - 1; i >= 0; i-- {
		l := locs[i]
		if blk, valid := parseBody(msg[l[2]:l[3]]); valid {
			return blk, strings.TrimSpace(msg[:l[0]] + msg[l[1]:]), true
		}
	}
	return Block{}, msg, false
}

func parseBody(body string) (Block, bool) {
	var b Block
	var key string
	text := map[string][]string{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := keyRe.FindStringSubmatch(line); m != nil {
			key = strings.ToLower(m[1])
			v := strings.TrimSpace(m[2])
			switch key {
			case "learning":
				if v != "" {
					b.Learnings = append(b.Learnings, v)
				}
			case "spawn":
				if v != "" {
					b.Spawn = append(b.Spawn, v)
				}
			default:
				text[key] = append(text[key], v)
			}
			continue
		}
		switch key {
		case "criteria":
			if m := critRe.FindStringSubmatch(line); m != nil {
				b.Criteria = append(b.Criteria, Criterion{Met: m[1] != " ", Text: strings.TrimSpace(m[2])})
			}
		case "summary", "state", "next":
			text[key] = append(text[key], strings.TrimSpace(line))
		}
	}
	join := func(k string) string { return strings.TrimSpace(strings.Join(text[k], "\n")) }
	b.Status = strings.ToLower(join("status"))
	if b.Status != "done" && b.Status != "failed" {
		return Block{}, false
	}
	b.Summary, b.State, b.Next = join("summary"), join("state"), join("next")
	return b, true
}
