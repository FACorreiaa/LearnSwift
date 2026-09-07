// Package leaderboard ranks learners who asked to be ranked.
//
// There are two boards and the reason there are two is the whole point. An
// agent can write correct Swift, so a single count of solved lessons says
// nothing about what the learner can do. Solo counts the lessons somebody
// worked out and typed themselves; Assisted counts everything else, including
// every submission that arrived over MCP.
//
// Neither number is a credential. There is no proctoring, no identity check,
// and no claim that an employer should read this. A learner determined to
// misreport has to sit in the editor and lie about work they could have simply
// done — which is more effort than the lesson, and costs them only the learning
// they came for.
package leaderboard

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	leaderboarddb "github.com/FACorreiaa/seshat/internal/leaderboard/db"
	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
)

const (
	// boardLimit bounds the page. Long enough that nobody who belongs on it is
	// cut off at the scale this runs at, short enough that the page cannot
	// become a database dump.
	boardLimit = 100

	minDisplayName = 3
	maxDisplayName = 24
)

// Standing is one learner's line on the board.
type Standing struct {
	DisplayName string
	Solo        int64
	Assisted    int64
}

type Service struct {
	q *leaderboarddb.Queries
}

func New(db leaderboarddb.DBTX) *Service {
	return &Service{q: leaderboarddb.New(db)}
}

// Boards returns the two rankings.
//
// One query and two orderings rather than two queries: the counts are the same
// counts, and computing them twice to sort them differently would be work done
// for the sake of the SQL rather than the reader.
//
// Computed on read rather than kept in a counter column. At this scale the
// query is instant, and a maintained total is a cache-invalidation bug waiting
// to be written — a number that lies is worse than a number that takes a
// moment.
func (s *Service) Boards(ctx context.Context) (solo, assisted []Standing, err error) {
	rows, err := s.q.Board(ctx, boardLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("leaderboard: board: %w", err)
	}

	solo = make([]Standing, 0, len(rows))
	for _, row := range rows {
		solo = append(solo, Standing{
			DisplayName: row.DisplayName,
			Solo:        row.Solo,
			Assisted:    row.Assisted,
		})
	}

	// The query already ordered by solo. The assisted view is the same rows
	// read the other way round.
	assisted = slices.Clone(solo)
	slices.SortStableFunc(assisted, func(a, b Standing) int {
		if a.Assisted != b.Assisted {
			return int(b.Assisted - a.Assisted)
		}
		return strings.Compare(a.DisplayName, b.DisplayName)
	})

	return solo, assisted, nil
}

// OptIn adds a learner to the boards, or renames one already on them.
//
// The same call for both, because choosing a name and changing it are the same
// decision made twice, and a separate rename route would only be a second place
// for the validation to drift.
func (s *Service) OptIn(ctx context.Context, userID uuid.UUID, displayName string) error {
	name, err := cleanDisplayName(displayName)
	if err != nil {
		return err
	}

	_, err = s.q.OptIn(ctx, leaderboarddb.OptInParams{UserID: userID, DisplayName: name})
	if err != nil {
		// 23505 is unique_violation. Letting the database decide means two
		// learners choosing the same name at the same moment cannot both win.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%w: somebody already uses that name", apperr.ErrValidation)
		}
		return fmt.Errorf("leaderboard: opt in: %w", err)
	}
	return nil
}

// OptOut removes a learner from the boards. Their attempts and progress are
// untouched: leaving a ranking is not leaving the course.
func (s *Service) OptOut(ctx context.Context, userID uuid.UUID) error {
	if _, err := s.q.OptOut(ctx, userID); err != nil {
		return fmt.Errorf("leaderboard: opt out: %w", err)
	}
	return nil
}

// Standing reports a learner's own opt-in, and whether they have one at all.
func (s *Service) Standing(ctx context.Context, userID uuid.UUID) (string, bool, error) {
	row, err := s.q.GetOptIn(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("leaderboard: get opt in: %w", err)
	}
	return row.DisplayName, true, nil
}

// cleanDisplayName decides what may appear on a page whose entire content is
// other people's names.
//
// Letters, digits, spaces, hyphens and underscores. Not a taste judgement: this
// rules out the two things that actually cause trouble — control and formatting
// characters that can rewrite the line they sit on, and homoglyph runs whose
// only purpose is to read as somebody else's name.
func cleanDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)

	if n := len([]rune(name)); n < minDisplayName || n > maxDisplayName {
		return "", fmt.Errorf("%w: a display name is between %d and %d characters",
			apperr.ErrValidation, minDisplayName, maxDisplayName)
	}

	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
		case r == ' ', r == '-', r == '_':
		default:
			return "", fmt.Errorf("%w: a display name can hold letters, digits, spaces, hyphens and underscores",
				apperr.ErrValidation)
		}
	}

	// Two spaces in a row are how one name is made to look like another while
	// being a different string to the unique index.
	if strings.Contains(name, "  ") {
		return "", fmt.Errorf("%w: a display name cannot hold two spaces in a row", apperr.ErrValidation)
	}

	return name, nil
}
