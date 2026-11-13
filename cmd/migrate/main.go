package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	command := "up"
	if len(os.Args) > 1 {
		command = strings.ToLower(strings.TrimSpace(os.Args[1]))
	}

	databaseURL := strings.TrimSpace(os.Getenv("GEO_MIGRATION_DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("GEO_MIGRATION_DATABASE_URL is required")
	}

	migrationsPath := strings.TrimSpace(os.Getenv("GEO_MIGRATIONS_PATH"))
	if migrationsPath == "" {
		migrationsPath = "db/migrations"
	}
	absolutePath, err := filepath.Abs(migrationsPath)
	if err != nil {
		log.Fatalf("resolve migrations path: %v", err)
	}
	sourceURL := "file://" + filepath.ToSlash(absolutePath)

	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		log.Fatalf("initialize migrations: %v", err)
	}
	defer func() {
		sourceErr, databaseErr := m.Close()
		if sourceErr != nil || databaseErr != nil {
			log.Printf("close migration resources: source=%v database=%v", sourceErr, databaseErr)
		}
	}()

	switch command {
	case "up":
		err = m.Up()
	case "down-one":
		err = m.Steps(-1)
	case "version":
		var version uint
		var dirty bool
		version, dirty, err = m.Version()
		if err == nil {
			fmt.Printf("version=%d dirty=%t\n", version, dirty)
		}
	default:
		log.Fatalf("unsupported command %q (use up, down-one, or version)", command)
	}

	if errors.Is(err, migrate.ErrNoChange) {
		fmt.Println("database schema is already current")
		return
	}
	if err != nil {
		log.Fatalf("migration %s failed: %v", command, err)
	}
	if command != "version" {
		fmt.Printf("migration %s completed\n", command)
	}
}
