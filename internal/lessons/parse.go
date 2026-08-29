// Package lessons turns the markdown under content/lessons into the lessons the
// app serves, and handles the routes that present them.
//
// The domain types themselves live in the leaf package internal/lessons/lesson,
// so the templates in web/lesson can name a Lesson without importing this
// package — which would be a cycle, since this package imports them to render.
//
// Lessons are files embedded in the binary, not rows in a table. A lesson is
// content under version control — it is reviewed, diffed and rolled back like
// code, and a deploy is the only thing that should change one. Putting them in
// the database would buy an editing UI nobody asked for and cost the ability to
// see what changed.
package lessons

import (
	"bytes"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"gopkg.in/yaml.v3"

	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
)

type frontmatter struct {
	Title      string            `yaml:"title"`
	Track      string            `yaml:"track"`
	Order      int               `yaml:"order"`
	Minutes    int               `yaml:"minutes"`
	Summary    string            `yaml:"summary"`
	Runtime    lesson.Runtime    `yaml:"runtime"`
	Starter    string            `yaml:"starter"`
	Hint       string            `yaml:"hint"`
	Solution   string            `yaml:"solution"`
	Assertions lesson.Assertions `yaml:"assertions"`
}

// Index is the parsed, ordered set of lessons, built once at startup.
type Index struct {
	tracks  []lesson.Track
	bySlug  map[string]lesson.Lesson
	ordered []lesson.Lesson
}

func (i *Index) Tracks() []lesson.Track { return i.tracks }
func (i *Index) All() []lesson.Lesson   { return i.ordered }
func (i *Index) Count() int             { return len(i.ordered) }

func (i *Index) Get(slug string) (lesson.Lesson, bool) {
	l, ok := i.bySlug[slug]
	return l, ok
}

// Next returns the lesson after slug in reading order, which is what the "keep
// going" link at the end of a lesson needs.
func (i *Index) Next(slug string) (lesson.Lesson, bool) {
	for n, l := range i.ordered {
		if l.Slug == slug && n+1 < len(i.ordered) {
			return i.ordered[n+1], true
		}
	}
	return lesson.Lesson{}, false
}

// trackTitles maps a track slug to its display name. Tracks are a closed set,
// so they live here rather than being inferred from whatever the lesson files
// happen to spell — a typo in one file would otherwise silently create a track.
var trackTitles = map[string]string{
	"swift-basics": "Swift Basics",
	"swiftui":      "SwiftUI Fundamentals",
	"concurrency":  "Concurrency",
	"patterns":     "Patterns",
}

// trackOrder is the order tracks are presented in, which is pedagogical rather
// than alphabetical.
var trackOrder = []string{"swift-basics", "swiftui", "concurrency", "patterns"}

func newMarkdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			// Swift samples are the entire point of the page, so they are
			// highlighted when the lesson is parsed rather than by shipping a
			// syntax highlighter to the browser.
			//
			// WithClasses emits class names instead of inline style attributes.
			// Inline styles cannot respond to the theme — they would bake a
			// light background and dark text into the markup, so every code
			// block would stay white in dark mode. The colours live in
			// web/assets/css/chroma.css instead, where both themes can be
			// expressed.
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
			),
		),
	)
}

// Parse builds an Index from a filesystem of markdown files.
//
// It takes an fs.FS rather than reading the embedded one directly so tests can
// parse a handful of fixtures without touching the real content.
func Parse(fsys fs.FS) (*Index, error) {
	names, err := fs.Glob(fsys, "*.md")
	if err != nil {
		return nil, fmt.Errorf("lesson: glob: %w", err)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("lesson: no lesson files found")
	}

	md := newMarkdown()
	byTrack := map[string][]lesson.Lesson{}
	index := &Index{bySlug: make(map[string]lesson.Lesson, len(names))}

	for _, name := range names {
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("lesson: read %s: %w", name, err)
		}

		slug := strings.TrimSuffix(name, ".md")
		l, err := parseOne(md, slug, raw)
		if err != nil {
			return nil, err
		}

		if _, dup := index.bySlug[l.Slug]; dup {
			return nil, fmt.Errorf("lesson: duplicate slug %q", l.Slug)
		}
		index.bySlug[l.Slug] = l
		byTrack[l.Track] = append(byTrack[l.Track], l)
	}

	for _, trackSlug := range trackOrder {
		lessons := byTrack[trackSlug]
		if len(lessons) == 0 {
			continue
		}
		sort.Slice(lessons, func(a, b int) bool {
			if lessons[a].Order != lessons[b].Order {
				return lessons[a].Order < lessons[b].Order
			}
			// A stable tiebreak, so two lessons that share an order number do
			// not swap places between builds.
			return lessons[a].Slug < lessons[b].Slug
		})

		index.tracks = append(index.tracks, lesson.Track{
			Slug:    trackSlug,
			Title:   trackTitles[trackSlug],
			Lessons: lessons,
		})
		index.ordered = append(index.ordered, lessons...)
		delete(byTrack, trackSlug)
	}

	// Anything left names a track that is not in trackOrder. Failing here is
	// the point: a lesson assigned to a misspelled track would otherwise be
	// parsed successfully and then never appear anywhere in the app.
	for trackSlug := range byTrack {
		return nil, fmt.Errorf("lesson: unknown track %q (known: %s)",
			trackSlug, strings.Join(trackOrder, ", "))
	}

	return index, nil
}

func parseOne(md goldmark.Markdown, slug string, raw []byte) (lesson.Lesson, error) {
	fmBytes, body, err := splitFrontmatter(raw)
	if err != nil {
		return lesson.Lesson{}, fmt.Errorf("lesson %s: %w", slug, err)
	}

	var fm frontmatter
	if err := yaml.Unmarshal(fmBytes, &fm); err != nil {
		return lesson.Lesson{}, fmt.Errorf("lesson %s: frontmatter: %w", slug, err)
	}

	switch {
	case fm.Title == "":
		return lesson.Lesson{}, fmt.Errorf("lesson %s: title is required", slug)
	case fm.Track == "":
		return lesson.Lesson{}, fmt.Errorf("lesson %s: track is required", slug)
	case fm.Minutes <= 0:
		return lesson.Lesson{}, fmt.Errorf("lesson %s: minutes must be positive", slug)
	case !fm.Runtime.Valid():
		// Deliberately required rather than defaulted. A lesson that silently
		// defaulted to executable would offer a Run button that could never
		// work — SwiftUI cannot be compiled to wasm at all — and one that
		// defaulted to "none" would quietly lose its exercise.
		return lesson.Lesson{}, fmt.Errorf(
			"lesson %s: runtime must be one of embedded, full, none (got %q)", slug, fm.Runtime)
	}

	var html bytes.Buffer
	if err := md.Convert(body, &html); err != nil {
		return lesson.Lesson{}, fmt.Errorf("lesson %s: render: %w", slug, err)
	}

	return lesson.Lesson{
		Slug:       slug,
		Title:      fm.Title,
		Track:      fm.Track,
		Order:      fm.Order,
		Minutes:    fm.Minutes,
		Summary:    fm.Summary,
		Runtime:    fm.Runtime,
		BodyHTML:   html.String(),
		Starter:    fm.Starter,
		Hint:       fm.Hint,
		Solution:   fm.Solution,
		Assertions: fm.Assertions,
	}, nil
}

var frontmatterDelim = []byte("---")

// splitFrontmatter separates the leading YAML block from the markdown body.
func splitFrontmatter(raw []byte) (fm, body []byte, err error) {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if !bytes.HasPrefix(trimmed, frontmatterDelim) {
		return nil, nil, fmt.Errorf("must begin with a --- frontmatter block")
	}

	rest := trimmed[len(frontmatterDelim):]
	// Look for the closing delimiter at the start of a line, so a --- used as a
	// horizontal rule inside the frontmatter's own strings cannot end it early.
	idx := bytes.Index(rest, append([]byte("\n"), frontmatterDelim...))
	if idx < 0 {
		return nil, nil, fmt.Errorf("frontmatter block is not closed with ---")
	}

	fm = rest[:idx]
	body = rest[idx+1+len(frontmatterDelim):]
	return fm, body, nil
}
