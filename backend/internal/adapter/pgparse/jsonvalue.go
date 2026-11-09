package pgparse

// The PL/pgSQL tree arrives as decoded JSON. A value of the wrong shape reads
// as absent, and an absent field is simply not walked.

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func asList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asInt(v any) (int, bool) {
	if n, ok := v.(float64); ok {
		return int(n), true
	}
	return 0, false
}
