package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/database"
	"github.com/google/uuid"
)

func main() {
	dsn := strings.TrimSpace(os.Getenv("GEO_DATABASE_DSN"))
	if dsn == "" {
		log.Fatal("GEO_DATABASE_DSN is required")
	}
	tenantID := strings.TrimSpace(os.Getenv("GEO_DEV_TENANT_ID"))
	if tenantID == "" {
		tenantID = "019a7f64-7c2c-7000-8000-000000000001"
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		log.Fatalf("GEO_DEV_TENANT_ID: %v", err)
	}
	actorID := strings.TrimSpace(os.Getenv("GEO_DEV_ACTOR_ID"))
	if actorID == "" {
		actorID = "019a7f64-7c2c-7000-8000-000000000002"
	}
	if _, err := uuid.Parse(actorID); err != nil {
		log.Fatalf("GEO_DEV_ACTOR_ID: %v", err)
	}

	db, err := database.Open(dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `
INSERT INTO tenants (id,name,status,plan_code,data_region,created_at,updated_at)
VALUES (UUID_TO_BIN(?),'Local Development','active','development','local',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE updated_at=VALUES(updated_at)`, tenantID)
	if err != nil {
		log.Fatalf("seed development tenant: %v", err)
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO users (id,oidc_subject,email_normalized,status,created_at,updated_at)
VALUES (UUID_TO_BIN(?),'local-development','developer@localhost.invalid','active',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE updated_at=VALUES(updated_at)`, actorID)
	if err != nil {
		log.Fatalf("seed development user: %v", err)
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO memberships (id,tenant_id,user_id,role,status,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'Owner','active',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE role='Owner',status='active',updated_at=VALUES(updated_at)`,
		"019a7f64-7c2c-7000-8000-000000000003", tenantID, actorID)
	if err != nil {
		log.Fatalf("seed development membership: %v", err)
	}
	log.Printf("development tenant and actor ready: %s / %s", tenantID, actorID)
}
