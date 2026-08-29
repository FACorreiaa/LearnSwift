package lesson

import (
	"strings"
	"testing"
)

// The case that started this: an exclamation mark inside a printed string is
// punctuation, and reading it as a force-unwrap fails a correct answer.
func TestPunctuationInsideAStringIsNotCode(t *testing.T) {
	code := `if let greeting {
    print("Hello, \(greeting)!")
}`

	if UsesToken(code, "!") {
		t.Error("the ! inside the string literal was read as a force unwrap")
	}
}

func TestARealForceUnwrapIsStillFound(t *testing.T) {
	if !UsesToken(`print(greeting!)`, "!") {
		t.Error("a force unwrap outside a string must be found")
	}
}

func TestATokenMentionedOnlyInACommentDoesNotCount(t *testing.T) {
	for _, code := range []string{
		"// remember to use await here\nlet x = 1",
		"/* await goes here */\nlet x = 1",
	} {
		if UsesToken(code, "await") {
			t.Errorf("a comment should not satisfy an assertion: %q", code)
		}
	}
}

func TestNestedBlockCommentsAreConsumedWhole(t *testing.T) {
	// Swift block comments nest, so a naive search for the first */ would stop
	// early and treat the tail as code.
	code := "/* outer /* inner */ still comment ! */ let x = 1"

	if UsesToken(code, "!") {
		t.Error("the ! is inside a nested block comment")
	}
	if !UsesToken(code, "let x") {
		t.Error("code after the comment must survive")
	}
}

func TestEscapedQuotesDoNotEndAStringEarly(t *testing.T) {
	code := `let s = "she said \"stop!\" loudly"`

	if UsesToken(code, "!") {
		t.Error("the escaped quotes ended the literal early, exposing its contents as code")
	}
}

func TestMultilineStringsAreStripped(t *testing.T) {
	code := "let s = \"\"\"\nnot code!\n\"\"\"\nlet y = 2"

	if UsesToken(code, "!") {
		t.Error("content of a multiline string was treated as code")
	}
	if !UsesToken(code, "let y") {
		t.Error("code after a multiline string must survive")
	}
}

// Offsets have to be preserved, or a future diagnostic that reports a line
// number from the stripped source would point at the wrong line.
func TestStrippingPreservesLengthAndLineCount(t *testing.T) {
	code := "let a = \"one\"\n// two\nlet b = 3\n/* four\nfive */\nlet c = 6"

	got := StripLiteralsAndComments(code)

	if len(got) != len(code) {
		t.Errorf("length changed: %d != %d", len(got), len(code))
	}
	if strings.Count(got, "\n") != strings.Count(code, "\n") {
		t.Errorf("line count changed: %d != %d", strings.Count(got, "\n"), strings.Count(code, "\n"))
	}
}

// An unterminated construct must not swallow the file or loop forever.
func TestUnterminatedConstructsTerminate(t *testing.T) {
	for name, code := range map[string]string{
		"string":        `let s = "never closed`,
		"multiline":     "let s = \"\"\"\nnever closed",
		"block comment": "/* never closed",
	} {
		t.Run(name, func(t *testing.T) {
			got := StripLiteralsAndComments(code)
			if len(got) != len(code) {
				t.Errorf("length changed: %d != %d", len(got), len(code))
			}
		})
	}
}
