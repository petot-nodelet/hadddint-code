package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/sukamoto/app/db/generated"
)

type sqliteStore struct {
	queries *generated.Queries
}

func (s *sqliteStore) Find(token string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := s.queries.FindSession(ctx, generated.FindSessionParams{
		ID: hexToken(token), ExpiresAt: time.Now().Unix(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("auth: find session: %w", err)
	}
	return data, true, nil
}

func (s *sqliteStore) Commit(token string, data []byte, expiry time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.queries.CommitSession(ctx, generated.CommitSessionParams{
		ID: hexToken(token), Data: data, ExpiresAt: expiry.Unix(),
	}); err != nil {
		return fmt.Errorf("auth: commit session: %w", err)
	}
	return nil
}

func (s *sqliteStore) Delete(token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.queries.DeleteSession(ctx, hexToken(token)); err != nil {
		return fmt.Errorf("auth: delete session: %w", err)
	}
	return nil
}

func hexToken(token string) string {
	return fmt.Sprintf("%x", hashToken(token))
}

var _ scs.Store = (*sqliteStore)(nil)
