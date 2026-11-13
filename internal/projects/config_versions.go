package projects

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type configSnapshot struct {
	SchemaVersion string               `json:"schemaVersion"`
	Project       snapshotProject      `json:"project"`
	Brand         snapshotBrand        `json:"brand"`
	Aliases       []snapshotAlias      `json:"aliases"`
	Products      []snapshotProduct    `json:"products"`
	Contexts      []snapshotContext    `json:"contexts"`
	Competitors   []snapshotCompetitor `json:"competitors"`
}

type snapshotProject struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Locale   string `json:"locale"`
	Region   string `json:"region"`
	Timezone string `json:"timezone"`
}

type snapshotBrand struct {
	ID             string `json:"id"`
	CanonicalName  string `json:"canonicalName"`
	OfficialDomain string `json:"officialDomain"`
	OfficialURL    string `json:"officialUrl"`
}

type snapshotAlias struct {
	ID      string  `json:"id"`
	Alias   string  `json:"alias"`
	Type    string  `json:"type"`
	Locale  *string `json:"locale"`
	Enabled bool    `json:"enabled"`
}

type snapshotProduct struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Description        *string `json:"description"`
	VerificationStatus string  `json:"verificationStatus"`
}

type snapshotContext struct {
	ID                 string `json:"id"`
	Type               string `json:"type"`
	Value              string `json:"value"`
	VerificationStatus string `json:"verificationStatus"`
}

type snapshotCompetitor struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Domain *string `json:"domain"`
	Status string  `json:"status"`
}

func (s *Store) PublishConfig(ctx context.Context, tenantID, projectID, actorID string) (ConfigVersion, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ConfigVersion{}, fmt.Errorf("begin config publication: %w", err)
	}
	defer tx.Rollback()

	snapshot, err := loadConfigSnapshot(ctx, tx, tenantID, projectID)
	if err != nil {
		return ConfigVersion{}, err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return ConfigVersion{}, fmt.Errorf("encode config snapshot: %w", err)
	}
	hash := sha256.Sum256(payload)
	hashHex := hex.EncodeToString(hash[:])

	var existing ConfigVersion
	err = tx.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(id),version,published_at FROM project_config_versions
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND content_hash=?`, tenantID, projectID, hash[:]).
		Scan(&existing.ID, &existing.Version, &existing.PublishedAt)
	if err == nil {
		existing.ProjectID, existing.ContentHash = projectID, hashHex
		if commitErr := tx.Commit(); commitErr != nil {
			return ConfigVersion{}, fmt.Errorf("commit config lookup: %w", commitErr)
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ConfigVersion{}, fmt.Errorf("find config snapshot: %w", err)
	}

	var nextVersion uint32
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM project_config_versions WHERE project_id=UUID_TO_BIN(?) FOR UPDATE`, projectID).Scan(&nextVersion); err != nil {
		return ConfigVersion{}, fmt.Errorf("allocate config version: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return ConfigVersion{}, fmt.Errorf("create config version id: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO project_config_versions (id,tenant_id,project_id,version,snapshot_json,content_hash,published_by,published_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,CAST(? AS JSON),?,UUID_TO_BIN(?),UTC_TIMESTAMP(6))`,
		id.String(), tenantID, projectID, nextVersion, payload, hash[:], actorID)
	if err != nil {
		return ConfigVersion{}, fmt.Errorf("insert config version: %w", err)
	}
	var publishedAt time.Time
	if err := tx.QueryRowContext(ctx, `SELECT published_at FROM project_config_versions WHERE id=UUID_TO_BIN(?)`, id.String()).Scan(&publishedAt); err != nil {
		return ConfigVersion{}, fmt.Errorf("read config publication time: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ConfigVersion{}, fmt.Errorf("commit config publication: %w", err)
	}
	return ConfigVersion{ID: id.String(), ProjectID: projectID, Version: nextVersion, ContentHash: hashHex, PublishedAt: publishedAt}, nil
}

func loadConfigSnapshot(ctx context.Context, tx *sql.Tx, tenantID, projectID string) (configSnapshot, error) {
	var snapshot configSnapshot
	var projectStatus string
	snapshot.SchemaVersion = "project-config-v1"
	err := tx.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(p.id),p.name,p.default_locale,p.region,p.timezone,
       BIN_TO_UUID(b.id),b.canonical_name,b.official_domain,w.base_url,p.status
FROM projects p
JOIN brands b ON b.tenant_id=p.tenant_id AND b.project_id=p.id
JOIN websites w ON w.tenant_id=p.tenant_id AND w.project_id=p.id
WHERE p.tenant_id=UUID_TO_BIN(?) AND p.id=UUID_TO_BIN(?)
ORDER BY b.created_at,w.created_at LIMIT 1 FOR UPDATE`, tenantID, projectID).Scan(
		&snapshot.Project.ID, &snapshot.Project.Name, &snapshot.Project.Locale, &snapshot.Project.Region, &snapshot.Project.Timezone,
		&snapshot.Brand.ID, &snapshot.Brand.CanonicalName, &snapshot.Brand.OfficialDomain, &snapshot.Brand.OfficialURL, &projectStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return configSnapshot{}, ErrNotFound
	}
	if err != nil {
		return configSnapshot{}, fmt.Errorf("load project snapshot: %w", err)
	}
	if projectStatus == "archived" {
		return configSnapshot{}, ErrArchived
	}

	if err := querySnapshotRows(ctx, tx, `SELECT BIN_TO_UUID(id),alias,alias_type,locale,enabled FROM brand_aliases WHERE tenant_id=UUID_TO_BIN(?) AND brand_id=UUID_TO_BIN(?) ORDER BY id`, []any{tenantID, snapshot.Brand.ID}, func(rows *sql.Rows) error {
		var value snapshotAlias
		if err := rows.Scan(&value.ID, &value.Alias, &value.Type, &value.Locale, &value.Enabled); err != nil {
			return err
		}
		snapshot.Aliases = append(snapshot.Aliases, value)
		return nil
	}); err != nil {
		return configSnapshot{}, fmt.Errorf("load aliases: %w", err)
	}
	if err := querySnapshotRows(ctx, tx, `SELECT BIN_TO_UUID(id),name,description,verification_status FROM products WHERE tenant_id=UUID_TO_BIN(?) AND brand_id=UUID_TO_BIN(?) ORDER BY id`, []any{tenantID, snapshot.Brand.ID}, func(rows *sql.Rows) error {
		var value snapshotProduct
		if err := rows.Scan(&value.ID, &value.Name, &value.Description, &value.VerificationStatus); err != nil {
			return err
		}
		snapshot.Products = append(snapshot.Products, value)
		return nil
	}); err != nil {
		return configSnapshot{}, fmt.Errorf("load products: %w", err)
	}
	if err := querySnapshotRows(ctx, tx, `SELECT BIN_TO_UUID(id),item_type,value,verification_status FROM brand_context_items WHERE tenant_id=UUID_TO_BIN(?) AND brand_id=UUID_TO_BIN(?) ORDER BY id`, []any{tenantID, snapshot.Brand.ID}, func(rows *sql.Rows) error {
		var value snapshotContext
		if err := rows.Scan(&value.ID, &value.Type, &value.Value, &value.VerificationStatus); err != nil {
			return err
		}
		snapshot.Contexts = append(snapshot.Contexts, value)
		return nil
	}); err != nil {
		return configSnapshot{}, fmt.Errorf("load contexts: %w", err)
	}
	if err := querySnapshotRows(ctx, tx, `SELECT BIN_TO_UUID(id),name,domain,status FROM competitors WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY id`, []any{tenantID, projectID}, func(rows *sql.Rows) error {
		var value snapshotCompetitor
		if err := rows.Scan(&value.ID, &value.Name, &value.Domain, &value.Status); err != nil {
			return err
		}
		snapshot.Competitors = append(snapshot.Competitors, value)
		return nil
	}); err != nil {
		return configSnapshot{}, fmt.Errorf("load competitors: %w", err)
	}
	if snapshot.Aliases == nil {
		snapshot.Aliases = []snapshotAlias{}
	}
	if snapshot.Products == nil {
		snapshot.Products = []snapshotProduct{}
	}
	if snapshot.Contexts == nil {
		snapshot.Contexts = []snapshotContext{}
	}
	if snapshot.Competitors == nil {
		snapshot.Competitors = []snapshotCompetitor{}
	}
	return snapshot, nil
}

func querySnapshotRows(ctx context.Context, tx *sql.Tx, query string, args []any, scan func(*sql.Rows) error) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
