package competitoraliases

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

var ErrNotFound = errors.New("competitor or alias was not found")
var ErrConflict = errors.New("competitor alias already exists")
var ErrInvalid = errors.New("competitor alias is invalid")

type Alias struct {
	ID    string `json:"id"`
	Alias string `json:"alias"`
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context, tenantID, competitorID string) ([]Alias, error) {
	if err := s.ensureCompetitor(ctx, tenantID, competitorID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),alias FROM competitor_aliases
WHERE tenant_id=UUID_TO_BIN(?) AND competitor_id=UUID_TO_BIN(?) AND enabled=TRUE ORDER BY alias`, tenantID, competitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Alias{}
	for rows.Next() {
		var item Alias
		if err := rows.Scan(&item.ID, &item.Alias); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Create(ctx context.Context, tenantID, competitorID, alias string) (Alias, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" || len([]rune(alias)) > 300 {
		return Alias{}, ErrInvalid
	}
	if err := s.ensureCompetitor(ctx, tenantID, competitorID); err != nil {
		return Alias{}, err
	}
	normalized := strings.ToLower(norm.NFKC.String(alias))
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO competitor_aliases (id,tenant_id,competitor_id,alias,alias_normalized,enabled)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,TRUE)`, id, tenantID, competitorID, alias, normalized); err != nil {
		if strings.Contains(err.Error(), "1062") {
			return Alias{}, ErrConflict
		}
		return Alias{}, err
	}
	var item Alias
	if err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),alias FROM competitor_aliases WHERE id=UUID_TO_BIN(?)`, id).Scan(&item.ID, &item.Alias); err != nil {
		return Alias{}, err
	}
	return item, nil
}

func (s *Store) Delete(ctx context.Context, tenantID, competitorID, aliasID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM competitor_aliases WHERE tenant_id=UUID_TO_BIN(?) AND competitor_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, competitorID, aliasID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ensureCompetitor(ctx context.Context, tenantID, competitorID string) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM competitors WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, competitorID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
