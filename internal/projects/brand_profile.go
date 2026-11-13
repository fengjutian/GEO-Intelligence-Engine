package projects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

var ErrAliasConflict = errors.New("brand alias already exists")

func (s *Store) GetBrandProfile(ctx context.Context, tenantID, projectID string) (BrandProfile, error) {
	var profile BrandProfile
	err := s.db.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(p.id),BIN_TO_UUID(b.id),b.canonical_name,b.official_domain
FROM projects p JOIN brands b ON b.tenant_id=p.tenant_id AND b.project_id=p.id
WHERE p.tenant_id=UUID_TO_BIN(?) AND p.id=UUID_TO_BIN(?)
ORDER BY b.created_at,b.id LIMIT 1`, tenantID, projectID).Scan(
		&profile.ProjectID, &profile.BrandID, &profile.CanonicalName, &profile.OfficialDomain)
	if errors.Is(err, sql.ErrNoRows) {
		return BrandProfile{}, ErrNotFound
	}
	if err != nil {
		return BrandProfile{}, fmt.Errorf("load brand profile: %w", err)
	}
	aliasRows, err := s.db.QueryContext(ctx, `
SELECT BIN_TO_UUID(id),alias,alias_type,locale,enabled,created_at
FROM brand_aliases WHERE tenant_id=UUID_TO_BIN(?) AND brand_id=UUID_TO_BIN(?) ORDER BY created_at,id`, tenantID, profile.BrandID)
	if err != nil {
		return BrandProfile{}, fmt.Errorf("query aliases: %w", err)
	}
	defer aliasRows.Close()
	for aliasRows.Next() {
		var alias BrandAlias
		if err := aliasRows.Scan(&alias.ID, &alias.Alias, &alias.AliasType, &alias.Locale, &alias.Enabled, &alias.CreatedAt); err != nil {
			return BrandProfile{}, fmt.Errorf("scan alias: %w", err)
		}
		profile.Aliases = append(profile.Aliases, alias)
	}
	if err := aliasRows.Err(); err != nil {
		return BrandProfile{}, fmt.Errorf("iterate aliases: %w", err)
	}
	productRows, err := s.db.QueryContext(ctx, `
SELECT BIN_TO_UUID(id),name,description,verification_status,COALESCE(source_type,''),created_at,updated_at
FROM products WHERE tenant_id=UUID_TO_BIN(?) AND brand_id=UUID_TO_BIN(?) ORDER BY created_at,id`, tenantID, profile.BrandID)
	if err != nil {
		return BrandProfile{}, fmt.Errorf("query products: %w", err)
	}
	defer productRows.Close()
	for productRows.Next() {
		var product Product
		if err := productRows.Scan(&product.ID, &product.Name, &product.Description, &product.VerificationStatus, &product.SourceType, &product.CreatedAt, &product.UpdatedAt); err != nil {
			return BrandProfile{}, fmt.Errorf("scan product: %w", err)
		}
		profile.Products = append(profile.Products, product)
	}
	if err := productRows.Err(); err != nil {
		return BrandProfile{}, fmt.Errorf("iterate products: %w", err)
	}
	competitorRows, err := s.db.QueryContext(ctx, `
SELECT BIN_TO_UUID(id),name,domain,status,created_at FROM competitors
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at,id`, tenantID, projectID)
	if err != nil {
		return BrandProfile{}, fmt.Errorf("query competitors: %w", err)
	}
	defer competitorRows.Close()
	for competitorRows.Next() {
		var competitor Competitor
		if err := competitorRows.Scan(&competitor.ID, &competitor.Name, &competitor.Domain, &competitor.Status, &competitor.CreatedAt); err != nil {
			return BrandProfile{}, fmt.Errorf("scan competitor: %w", err)
		}
		profile.Competitors = append(profile.Competitors, competitor)
	}
	if err := competitorRows.Err(); err != nil {
		return BrandProfile{}, fmt.Errorf("iterate competitors: %w", err)
	}
	if profile.Aliases == nil {
		profile.Aliases = []BrandAlias{}
	}
	if profile.Products == nil {
		profile.Products = []Product{}
	}
	if profile.Competitors == nil {
		profile.Competitors = []Competitor{}
	}
	return profile, nil
}

func (s *Store) CreateCompetitor(ctx context.Context, tenantID, projectID string, input CompetitorCreateInput) (Competitor, error) {
	var projectStatus, canonicalName string
	err := s.db.QueryRowContext(ctx, `
SELECT p.status,b.canonical_name FROM projects p JOIN brands b ON b.tenant_id=p.tenant_id AND b.project_id=p.id
WHERE p.tenant_id=UUID_TO_BIN(?) AND p.id=UUID_TO_BIN(?) ORDER BY b.created_at,b.id LIMIT 1`, tenantID, projectID).Scan(&projectStatus, &canonicalName)
	if errors.Is(err, sql.ErrNoRows) {
		return Competitor{}, ErrNotFound
	}
	if err != nil {
		return Competitor{}, fmt.Errorf("load competitor context: %w", err)
	}
	if projectStatus == "archived" {
		return Competitor{}, ErrArchived
	}
	normalized := NormalizeEntityName(input.Name)
	if normalized == NormalizeEntityName(canonicalName) {
		return Competitor{}, ErrAliasConflict
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Competitor{}, fmt.Errorf("create competitor id: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO competitors (id,tenant_id,project_id,name,name_normalized,domain,status,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,'active',UTC_TIMESTAMP(6))`,
		id.String(), tenantID, projectID, input.Name, normalized, input.Domain)
	if err != nil {
		return Competitor{}, fmt.Errorf("insert competitor: %w", err)
	}
	var competitor Competitor
	err = s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),name,domain,status,created_at FROM competitors WHERE id=UUID_TO_BIN(?)`, id.String()).
		Scan(&competitor.ID, &competitor.Name, &competitor.Domain, &competitor.Status, &competitor.CreatedAt)
	if err != nil {
		return Competitor{}, fmt.Errorf("read competitor: %w", err)
	}
	return competitor, nil
}

func (s *Store) CreateAlias(ctx context.Context, tenantID, projectID string, input AliasCreateInput) (BrandAlias, error) {
	brandID, status, err := s.brandContext(ctx, tenantID, projectID)
	if err != nil {
		return BrandAlias{}, err
	}
	if status == "archived" {
		return BrandAlias{}, ErrArchived
	}
	id, err := uuid.NewV7()
	if err != nil {
		return BrandAlias{}, fmt.Errorf("create alias id: %w", err)
	}
	normalized := NormalizeEntityName(input.Alias)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO brand_aliases (id,tenant_id,brand_id,alias,alias_normalized,alias_type,locale,enabled,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,TRUE,UTC_TIMESTAMP(6))`,
		id.String(), tenantID, brandID, input.Alias, normalized, input.AliasType, input.Locale)
	if err != nil {
		return BrandAlias{}, fmt.Errorf("insert alias: %w", err)
	}
	var result BrandAlias
	err = s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),alias,alias_type,locale,enabled,created_at FROM brand_aliases WHERE id=UUID_TO_BIN(?)`, id.String()).
		Scan(&result.ID, &result.Alias, &result.AliasType, &result.Locale, &result.Enabled, &result.CreatedAt)
	if err != nil {
		return BrandAlias{}, fmt.Errorf("read alias: %w", err)
	}
	return result, nil
}

func (s *Store) SetAliasEnabled(ctx context.Context, tenantID, projectID, aliasID string, enabled bool) (BrandAlias, error) {
	brandID, status, err := s.brandContext(ctx, tenantID, projectID)
	if err != nil {
		return BrandAlias{}, err
	}
	if status == "archived" {
		return BrandAlias{}, ErrArchived
	}
	result, err := s.db.ExecContext(ctx, `UPDATE brand_aliases SET enabled=? WHERE id=UUID_TO_BIN(?) AND tenant_id=UUID_TO_BIN(?) AND brand_id=UUID_TO_BIN(?)`, enabled, aliasID, tenantID, brandID)
	if err != nil {
		return BrandAlias{}, fmt.Errorf("update alias: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return BrandAlias{}, fmt.Errorf("read alias update: %w", err)
	}
	if count == 0 {
		return BrandAlias{}, ErrNotFound
	}
	var alias BrandAlias
	err = s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),alias,alias_type,locale,enabled,created_at FROM brand_aliases WHERE id=UUID_TO_BIN(?)`, aliasID).
		Scan(&alias.ID, &alias.Alias, &alias.AliasType, &alias.Locale, &alias.Enabled, &alias.CreatedAt)
	if err != nil {
		return BrandAlias{}, fmt.Errorf("read updated alias: %w", err)
	}
	return alias, nil
}

func (s *Store) CreateProduct(ctx context.Context, tenantID, projectID string, input ProductCreateInput) (Product, error) {
	brandID, status, err := s.brandContext(ctx, tenantID, projectID)
	if err != nil {
		return Product{}, err
	}
	if status == "archived" {
		return Product{}, ErrArchived
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Product{}, fmt.Errorf("create product id: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO products (id,tenant_id,brand_id,name,description,verification_status,source_type,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,'human_verified','manual',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		id.String(), tenantID, brandID, input.Name, input.Description)
	if err != nil {
		return Product{}, fmt.Errorf("insert product: %w", err)
	}
	var product Product
	err = s.db.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(id),name,description,verification_status,source_type,created_at,updated_at FROM products WHERE id=UUID_TO_BIN(?)`, id.String()).
		Scan(&product.ID, &product.Name, &product.Description, &product.VerificationStatus, &product.SourceType, &product.CreatedAt, &product.UpdatedAt)
	if err != nil {
		return Product{}, fmt.Errorf("read product: %w", err)
	}
	return product, nil
}

func (s *Store) brandContext(ctx context.Context, tenantID, projectID string) (string, string, error) {
	var brandID, status string
	err := s.db.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(b.id),p.status FROM projects p JOIN brands b ON b.tenant_id=p.tenant_id AND b.project_id=p.id
WHERE p.tenant_id=UUID_TO_BIN(?) AND p.id=UUID_TO_BIN(?) ORDER BY b.created_at,b.id LIMIT 1`, tenantID, projectID).Scan(&brandID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("load brand context: %w", err)
	}
	return brandID, status, nil
}

func NormalizeEntityName(value string) string {
	value = norm.NFKC.String(strings.TrimSpace(value))
	value = strings.ToLower(value)
	var output strings.Builder
	spacePending := false
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			spacePending = output.Len() > 0
			continue
		}
		if spacePending {
			output.WriteByte(' ')
			spacePending = false
		}
		output.WriteRune(r)
	}
	return output.String()
}
