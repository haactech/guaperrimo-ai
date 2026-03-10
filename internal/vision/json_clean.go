package vision

import "strings"

// cleanJSON strips markdown code fences and repairs unescaped interior quotes
// that LLMs sometimes produce in JSON output.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	// Strip code fences (```json ... ```)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i != -1 {
			s = s[i+1:]
		}
		if i := strings.LastIndex(s, "```"); i != -1 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	// Repair unescaped quotes inside JSON string values.
	// In valid JSON, a closing " is always followed by , } ] or : (after whitespace).
	// If a " inside a string is followed by anything else, it's an interior quote
	// that the LLM forgot to escape.
	s = repairJSONQuotes(s)
	return s
}

func repairJSONQuotes(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if escaped {
			buf.WriteByte(c)
			escaped = false
			continue
		}

		if c == '\\' && inString {
			buf.WriteByte(c)
			escaped = true
			continue
		}

		if c == '"' {
			if !inString {
				inString = true
				buf.WriteByte(c)
			} else {
				// Inside a string — is this the real close or an unescaped interior quote?
				// In valid JSON, after a string-closing ", the next non-whitespace
				// char must be one of: , } ] : or EOF.
				rest := strings.TrimSpace(s[i+1:])
				if len(rest) == 0 || rest[0] == ',' || rest[0] == '}' || rest[0] == ']' || rest[0] == ':' {
					// Legitimate string close
					inString = false
					buf.WriteByte(c)
				} else {
					// Interior quote — escape it
					buf.WriteString(`\"`)
				}
			}
		} else {
			buf.WriteByte(c)
		}
	}
	return buf.String()
}
