package sqlguard

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

type kind int

const (
	kWord    kind = iota // unquoted identifier or keyword; Text is lower-cased
	kQIdent              // "quoted identifier"; Text is the name without quotes
	kString              // any string literal
	kNumber              // numeric literal
	kPunct               // a single punctuation character
	kComment             // -- or /* */ comment, removed from the output
)

type token struct {
	kind       kind
	text       string
	start, end int // byte offsets in the source
}

var errUnterminated = errors.New("unterminated string, identifier or comment")

// lex splits PostgreSQL source into tokens. It follows the server's rules for
// the parts that decide where code ends and data begins: standard strings
// ('' escapes), E'' strings (backslash escapes), dollar quotes, quoted
// identifiers, and nested block comments.
func lex(src string) ([]token, error) {
	var out []token
	i := 0
	for i < len(src) {
		r, w := utf8.DecodeRuneInString(src[i:])
		start := i
		switch {
		case unicode.IsSpace(r):
			i += w
			continue

		case strings.HasPrefix(src[i:], "--"):
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				i = len(src)
			} else {
				i += end + 1
			}
			out = append(out, token{kComment, "", start, i})

		case strings.HasPrefix(src[i:], "/*"):
			depth := 0
			for i < len(src) {
				if strings.HasPrefix(src[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(src[i:], "*/") {
					depth--
					i += 2
					if depth == 0 {
						break
					}
				} else {
					i++
				}
			}
			if depth != 0 {
				return nil, errUnterminated
			}
			out = append(out, token{kComment, "", start, i})

		case (r == 'e' || r == 'E') && i+1 < len(src) && src[i+1] == '\'':
			end, ok := scanEscapeString(src, i+1)
			if !ok {
				return nil, errUnterminated
			}
			i = end
			out = append(out, token{kString, "", start, i})

		case r == '\'':
			end, ok := scanQuoted(src, i, '\'')
			if !ok {
				return nil, errUnterminated
			}
			i = end
			out = append(out, token{kString, "", start, i})

		case r == '"':
			end, ok := scanQuoted(src, i, '"')
			if !ok {
				return nil, errUnterminated
			}
			name := strings.ReplaceAll(src[i+1:end-1], `""`, `"`)
			i = end
			out = append(out, token{kQIdent, name, start, i})

		case r == '$':
			if tag, ok := dollarTag(src, i); ok {
				body := strings.Index(src[i+len(tag):], tag)
				if body < 0 {
					return nil, errUnterminated
				}
				i += len(tag) + body + len(tag)
				out = append(out, token{kString, "", start, i})
			} else {
				i += w
				out = append(out, token{kPunct, "$", start, i})
			}

		case unicode.IsDigit(r) || (r == '.' && i+1 < len(src) && isDigit(src[i+1])):
			i = scanNumber(src, i)
			out = append(out, token{kNumber, src[start:i], start, i})

		case unicode.IsLetter(r) || r == '_':
			for i < len(src) {
				r2, w2 := utf8.DecodeRuneInString(src[i:])
				if !(unicode.IsLetter(r2) || unicode.IsDigit(r2) || r2 == '_' || r2 == '$') {
					break
				}
				i += w2
			}
			out = append(out, token{kWord, strings.ToLower(src[start:i]), start, i})

		default:
			i += w
			out = append(out, token{kPunct, string(r), start, i})
		}
	}
	return out, nil
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// scanQuoted scans a '...' or "..." starting at the opening quote, where a
// doubled quote is an escaped quote. It returns the offset after the closing quote.
func scanQuoted(src string, i int, q byte) (int, bool) {
	i++
	for i < len(src) {
		if src[i] == q {
			if i+1 < len(src) && src[i+1] == q {
				i += 2
				continue
			}
			return i + 1, true
		}
		i++
	}
	return 0, false
}

// scanEscapeString scans E'...' from its opening quote: a backslash escapes
// the next byte, and '' is also an escaped quote.
func scanEscapeString(src string, i int) (int, bool) {
	i++
	for i < len(src) {
		switch src[i] {
		case '\\':
			i += 2
			continue
		case '\'':
			if i+1 < len(src) && src[i+1] == '\'' {
				i += 2
				continue
			}
			return i + 1, true
		}
		i++
	}
	return 0, false
}

// dollarTag returns "$tag$" or "$$" if a dollar quote starts at i.
func dollarTag(src string, i int) (string, bool) {
	j := i + 1
	for j < len(src) {
		r, w := utf8.DecodeRuneInString(src[j:])
		if r == '$' {
			tag := src[i : j+1]
			if len(tag) > 2 && unicode.IsDigit([]rune(tag[1:])[0]) {
				return "", false // $1 is a parameter, not a tag
			}
			return tag, true
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			return "", false
		}
		j += w
	}
	return "", false
}

func scanNumber(src string, i int) int {
	for i < len(src) && (isDigit(src[i]) || src[i] == '.' || src[i] == '_') {
		i++
	}
	if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
		j := i + 1
		if j < len(src) && (src[j] == '+' || src[j] == '-') {
			j++
		}
		if j < len(src) && isDigit(src[j]) {
			i = j
			for i < len(src) && isDigit(src[i]) {
				i++
			}
		}
	}
	return i
}
