package cmdmap

import "strings"

// Entry describes how an HTTP request maps to an orbit CLI command.
type Entry struct {
	Command     string // empty when there is no CLI equivalent
	Summary     string // human-readable description
	HasCLI      bool
	UserAction  bool   // false means middleware skips recording
	PathPattern string // used for gap deduplication
}

// Rule pairs a method+pattern with a builder that produces an Entry.
type Rule struct {
	Method  string // "POST", "PUT", "GET", or "*"
	Pattern string // path with :param segments
	Build   func(params map[string]string, body []byte) Entry
}

var rules []Rule

// Resolve returns the Entry for the first matching rule. A rule method of "*"
// matches any method. Unknown paths return a zero Entry with UserAction=false.
func Resolve(method, path string, body []byte) Entry {
	for _, r := range rules {
		if r.Method != "*" && r.Method != method {
			continue
		}
		params, ok := matchPattern(r.Pattern, path)
		if !ok {
			continue
		}
		entry := r.Build(params, body)
		entry.PathPattern = r.Pattern
		return entry
	}
	return Entry{}
}

func matchPattern(pattern, path string) (map[string]string, bool) {
	pp := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
	pa := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(pp) != len(pa) {
		return nil, false
	}
	params := map[string]string{}
	for i, seg := range pp {
		if strings.HasPrefix(seg, ":") {
			if pa[i] == "" {
				return nil, false
			}
			params[seg[1:]] = pa[i]
			continue
		}
		if seg != pa[i] {
			return nil, false
		}
	}
	return params, true
}

func setRulesForTest(rs []Rule) func() {
	prev := rules
	rules = rs
	return func() { rules = prev }
}
