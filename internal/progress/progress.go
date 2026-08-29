// Package progress records what a learner has read and attempted.
package progress

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	progressdb "github.com/FACorreiaa/seshat/internal/progress/db"
)

type Status string

const (
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

type LessonProgress struct {
	LessonSlug  string
	Status      Status
	StartedAt   time.Time
	CompletedAt *time.Time
}

func (p LessonProgress) IsCompleted() bool { return p.Status == StatusCompleted }

type Attempt struct {
	ID         uuid.UUID
	LessonSlug string
	Code       string
	Passed     bool
	CreatedAt  time.Time
}

type Service struct {
	q *progressdb.Queries
}

func New(db progressdb.DBTX) *Service {
	return &Service{q: progressdb.New(db)}
}

// Start marks a lesson as opened, without disturbing one already completed.
func (s *Service) Start(ctx context.Context, userID uuid.UUID, slug string) error {
	_, err := s.q.UpsertLessonProgress(ctx, progressdb.UpsertLessonProgressParams{
		UserID:     userID,
		LessonSlug: slug,
		Status:     string(StatusInProgress),
	})
	if err != nil {
		return fmt.Errorf("progress: start %s: %w", slug, err)
	}
	return nil
}

func (s *Service) Complete(ctx context.Context, userID uuid.UUID, slug string) error {
	_, err := s.q.UpsertLessonProgress(ctx, progressdb.UpsertLessonProgressParams{
		UserID:      userID,
		LessonSlug:  slug,
		Status:      string(StatusCompleted),
		CompletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("progress: complete %s: %w", slug, err)
	}
	return nil
}

// Get returns the progress for one lesson. A learner who has never opened it
// has no row, which is not an error — it is the ordinary starting state, so it
// comes back as a zero LessonProgress and false.
func (s *Service) Get(ctx context.Context, userID uuid.UUID, slug string) (LessonProgress, bool, error) {
	row, err := s.q.GetLessonProgress(ctx, progressdb.GetLessonProgressParams{
		UserID:     userID,
		LessonSlug: slug,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LessonProgress{}, false, nil
		}
		return LessonProgress{}, false, fmt.Errorf("progress: get %s: %w", slug, err)
	}
	return toLessonProgress(row), true, nil
}

// ForUser returns every lesson's progress keyed by slug, so a page rendering a
// list of lessons can look each one up without a query per lesson.
func (s *Service) ForUser(ctx context.Context, userID uuid.UUID) (map[string]LessonProgress, error) {
	rows, err := s.q.ListProgressForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("progress: list: %w", err)
	}

	out := make(map[string]LessonProgress, len(rows))
	for _, row := range rows {
		out[row.LessonSlug] = toLessonProgress(row)
	}
	return out, nil
}

func (s *Service) RecordAttempt(ctx context.Context, userID uuid.UUID, slug, code string, passed bool) (Attempt, error) {
	row, err := s.q.RecordAttempt(ctx, progressdb.RecordAttemptParams{
		UserID:     userID,
		LessonSlug: slug,
		Code:       code,
		Passed:     passed,
	})
	if err != nil {
		return Attempt{}, fmt.Errorf("progress: record attempt: %w", err)
	}
	return Attempt{
		ID:         row.ID,
		LessonSlug: row.LessonSlug,
		Code:       row.Code,
		Passed:     row.Passed,
		CreatedAt:  row.CreatedAt,
	}, nil
}

func (s *Service) Attempts(ctx context.Context, userID uuid.UUID, slug string, limit int32) ([]Attempt, error) {
	rows, err := s.q.ListAttempts(ctx, progressdb.ListAttemptsParams{
		UserID:     userID,
		LessonSlug: slug,
		Limit:      limit,
	})
	if err != nil {
		return nil, fmt.Errorf("progress: attempts: %w", err)
	}

	out := make([]Attempt, 0, len(rows))
	for _, row := range rows {
		out = append(out, Attempt{
			ID:         row.ID,
			LessonSlug: row.LessonSlug,
			Code:       row.Code,
			Passed:     row.Passed,
			CreatedAt:  row.CreatedAt,
		})
	}
	return out, nil
}

// toLessonProgress converts sqlc's pgtype at the edge of the slice, so the
// generated types never reach a handler or a template. See sqlc.yaml for why
// the nullable column is a pgtype rather than a *time.Time.
func toLessonProgress(row progressdb.UserLesson) LessonProgress {
	p := LessonProgress{
		LessonSlug: row.LessonSlug,
		Status:     Status(row.Status),
		StartedAt:  row.StartedAt,
	}
	if row.CompletedAt.Valid {
		completed := row.CompletedAt.Time
		p.CompletedAt = &completed
	}
	return p
}
