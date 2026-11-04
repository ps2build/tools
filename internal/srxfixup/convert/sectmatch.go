package convert

// sectNameMatch returns 0 when name matches pattern (an exact match, or
// a match against a pattern's own trailing "*" suffix wildcard), nonzero
// otherwise - a strcmp-shaped return, not a bool, since call sites use
// `sectNameMatch(...) == 0` for "matches".
func sectNameMatch(pattern, name string) int {
	i := 0
	for i < len(pattern) && i < len(name) && pattern[i] != '*' {
		if name[i] > pattern[i] {
			return -1
		}
		if name[i] < pattern[i] {
			return 1
		}
		i++
	}
	p, n := pattern[i:], name[i:]
	if p == "" && n == "" {
		return 0
	}
	if p == "*" {
		return 0
	}
	if len(p) == 0 || p[0] != '*' {
		return strCmp(p, n)
	}
	suf := p[1:]
	if len(n) < len(suf) {
		return strCmp(suf, n)
	}
	return strCmp(suf, n[len(n)-len(suf):])
}

func strCmp(a, b string) int {
	if a == b {
		return 0
	}
	if a < b {
		return -1
	}
	return 1
}
