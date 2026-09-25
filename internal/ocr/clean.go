package ocr

import "strings"

// CleanText trims markdown fences, keeps at most one blank or separator-only
// line in a row, and drops trailing separators so stored OCR is not padded
// with receipt-divider noise.
func CleanText(s string) string {
	s = trimOCRFences(s)
	if s == "" {
		return ""
	}

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	prevBlank := false
	prevSep := false

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			if prevBlank {
				continue
			}

			out = append(out, "")
			prevBlank = true
			prevSep = false

			continue
		}

		if isSeparatorLine(trimmed) {
			if prevSep {
				continue
			}

			out = append(out, trimmed)
			prevSep = true
			prevBlank = false

			continue
		}

		out = append(out, line)
		prevBlank = false
		prevSep = false
	}

	for len(out) > 0 {
		last := strings.TrimSpace(out[len(out)-1])
		if last == "" || isSeparatorLine(last) {
			out = out[:len(out)-1]

			continue
		}

		break
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}

func isSeparatorLine(s string) bool {
	return s != "" && strings.Trim(s, "-|: ") == ""
}
