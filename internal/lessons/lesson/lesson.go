// Package lesson holds the domain types for a lesson.
//
// It is a leaf: it imports nothing from the rest of the application, so both
// the lessons slice and the templates that render it can depend on it without
// depending on each other. Putting Lesson in the slice itself created an import
// cycle the moment a template needed to name the type.
package lesson

// Assertions decide whether an exercise submission passes.
//
// They live in the lesson's own frontmatter so the check and the prose
// explaining it are edited together — a lesson whose text and test disagree is
// worse than one with no test at all. Phase 2 runs the code; Phase 3 enforces
// these.
type Assertions struct {
	OutputContains []string `yaml:"output_contains"`
	OutputEquals   string   `yaml:"output_equals"`
	MustDeclare    []string `yaml:"must_declare"`
	MustNotUse     []string `yaml:"must_not_use"`
}

// NeedsExecution reports whether any assertion can only be settled by running
// the code. Those are evaluated by the compile pipeline, not the static
// validator, so a submission satisfying every *static* assertion of such a
// lesson has not yet been shown to be correct — it has only failed to be shown
// wrong. Telling a learner "that's right" on that basis would be a lie the
// first time their unfinished starter passed.
func (a Assertions) NeedsExecution() bool {
	return len(a.OutputContains) > 0 || a.OutputEquals != ""
}

// HasStatic reports whether anything can be checked without running the code.
// This is what decides whether an exercise gets a Check button, and it is
// deliberately independent of Runtime: SwiftUI cannot be executed anywhere, but
// its source parses like any other Swift, so `must_declare: "@State"` is a
// perfectly answerable question.
func (a Assertions) HasStatic() bool {
	return len(a.MustDeclare) > 0 || len(a.MustNotUse) > 0
}

func (a Assertions) Empty() bool {
	return len(a.OutputContains) == 0 && a.OutputEquals == "" &&
		len(a.MustDeclare) == 0 && len(a.MustNotUse) == 0
}

// Runtime says how a lesson's exercise can be checked, which is decided by what
// the Swift toolchain can actually target.
type Runtime string

const (
	// RuntimeEmbedded compiles against the embedded Swift SDK: ~38 KB of wasm
	// instead of ~1.9 MB. It has no concurrency runtime and no Unicode
	// normalization tables, so no async/await and no string sorting.
	RuntimeEmbedded Runtime = "embedded"

	// RuntimeFull compiles against the complete standard library. Everything
	// works; the artifact is roughly fifty times larger. Required for async
	// /await and for anything doing real string comparison.
	RuntimeFull Runtime = "full"

	// RuntimeNone means the exercise cannot be executed at all, only checked
	// statically. SwiftUI is the reason this exists: it is a closed-source
	// Apple framework with no Linux or WebAssembly build, so no amount of
	// toolchain work will run a SwiftUI view here.
	RuntimeNone Runtime = "none"
)

func (r Runtime) Executable() bool { return r == RuntimeEmbedded || r == RuntimeFull }

func (r Runtime) Valid() bool {
	switch r {
	case RuntimeEmbedded, RuntimeFull, RuntimeNone:
		return true
	}
	return false
}

type Lesson struct {
	Slug    string
	Title   string
	Track   string
	Order   int
	Minutes int
	Summary string

	// Runtime decides whether this lesson gets a Run button. Declared per
	// lesson rather than inferred from the track, because the constraint is
	// about what the code uses, not what it teaches.
	Runtime Runtime

	// BodyHTML is rendered once at startup. It comes from files in this
	// repository, never from user input, so it is trusted markup by
	// construction — which is the only reason a template writes it unescaped.
	BodyHTML string

	// Body is the same text before rendering. Kept because HTML is the wrong
	// format for a reader that is not a browser: an agent handed markup spends
	// tokens on tags, and the code fences a lesson leans on survive markdown
	// far better than they survive a <pre>.
	Body string

	Starter string

	// Hint is one sentence pointing at the shape of the answer without giving
	// it away, offered after a couple of failed attempts. It is held back until
	// then by the handler rather than hidden by the template: a hint sitting in
	// the DOM behind a CSS rule is a hint, revealed.
	Hint string

	// Solution is not sent to the browser with the lesson. It exists so a test
	// can prove the assertions actually accept a correct answer, and so a
	// learner who is genuinely stuck can ask for it — which is a separate
	// request, gated on having tried, and recorded when it is granted.
	//
	// The default of withholding it is right. Withholding it absolutely is not:
	// a learner stuck on their fourth attempt with no way forward does not
	// persevere, they leave.
	Solution   string
	Assertions Assertions
}

type Track struct {
	Slug    string
	Title   string
	Lessons []Lesson
}
