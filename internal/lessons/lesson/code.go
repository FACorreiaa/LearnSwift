package lesson

import "strings"

// StripLiteralsAndComments blanks out Swift string literals and comments,
// leaving everything else in place at its original offset.
//
// Assertions like `must_not_use: "!"` are substring searches, and a substring
// search over raw source is wrong in a way that is easy to miss: the `!` in
// `print("Hello, \(name)!")` is punctuation inside a string, not a force
// unwrap. Checking the stripped form instead means an assertion is about the
// code the reader wrote, not about the prose they printed.
//
// Replaced characters become spaces rather than being deleted so that offsets,
// and therefore line and column numbers, still line up with the original.
func StripLiteralsAndComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))

	// blank copies n characters as spaces, preserving newlines so line numbers
	// survive a multi-line literal or block comment.
	blank := func(s string) {
		for _, r := range s {
			if r == '\n' {
				out.WriteByte('\n')
			} else {
				out.WriteByte(' ')
			}
		}
	}

	for i := 0; i < len(src); {
		rest := src[i:]

		switch {
		case strings.HasPrefix(rest, `"""`):
			end := indexAfter(rest, 3, `"""`)
			if end < 0 {
				blank(rest)
				return out.String()
			}
			blank(rest[:end+3])
			i += end + 3

		case rest[0] == '"':
			n := scanStringLiteral(rest)
			blank(rest[:n])
			i += n

		case strings.HasPrefix(rest, "//"):
			end := strings.IndexByte(rest, '\n')
			if end < 0 {
				blank(rest)
				return out.String()
			}
			blank(rest[:end])
			i += end

		case strings.HasPrefix(rest, "/*"):
			// Swift block comments nest, so this cannot just search for the
			// first */.
			n := scanBlockComment(rest)
			blank(rest[:n])
			i += n

		default:
			out.WriteByte(src[i])
			i++
		}
	}

	return out.String()
}

// scanStringLiteral returns the length of the literal starting at src[0] == '"',
// honouring backslash escapes. An unterminated literal consumes to end of line,
// which is what the compiler would also treat as an error rather than letting it
// swallow the rest of the file.
func scanStringLiteral(src string) int {
	for i := 1; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++ // skip whatever was escaped, including an escaped quote
		case '"':
			return i + 1
		case '\n':
			return i
		}
	}
	return len(src)
}

func scanBlockComment(src string) int {
	depth := 0
	for i := 0; i < len(src)-1; i++ {
		switch {
		case src[i] == '/' && src[i+1] == '*':
			depth++
			i++
		case src[i] == '*' && src[i+1] == '/':
			depth--
			i++
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(src)
}

func indexAfter(s string, from int, sub string) int {
	if from >= len(s) {
		return -1
	}
	idx := strings.Index(s[from:], sub)
	if idx < 0 {
		return -1
	}
	return from + idx
}

// UsesToken reports whether the code actually uses token, ignoring anything
// inside strings and comments.
func UsesToken(code, token string) bool {
	return strings.Contains(StripLiteralsAndComments(code), token)
}
