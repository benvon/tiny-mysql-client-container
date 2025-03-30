package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-mysql-org/go-mysql/client"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/spf13/pflag"
)

var (
	host         = pflag.StringP("host", "h", "localhost", "MySQL host")
	port         = pflag.Int32P("port", "p", 3306, "MySQL port")
	user         = pflag.StringP("user", "u", "root", "MySQL user")
	password     = pflag.StringP("password", "P", "", "MySQL password")
	database     = pflag.StringP("database", "d", "", "MySQL database")
	file         = pflag.StringP("file", "f", "", "SQL file to execute")
	version      = pflag.StringP("version", "v", "", "Migration version")
	status       = pflag.BoolP("status", "s", false, "Show migration status")
	rollback     = pflag.StringP("rollback", "r", "", "Rollback to version")
	validate     = pflag.BoolP("validate", "c", false, "Validate migration files")
	templateName = pflag.StringP("template", "t", "", "Template to use for new migration")
	interactive  = pflag.BoolP("interactive", "i", false, "Run in interactive mode")
	depends      = pflag.StringP("depends", "D", "", "Comma-separated list of migration dependencies")
	dryRun       = pflag.BoolP("dry-run", "n", false, "Show what would be done without making changes")
)

const migrationsTable = `
CREATE TABLE IF NOT EXISTS __EFMigrationsHistory (
    MigrationId VARCHAR(150) NOT NULL,
    ProductVersion VARCHAR(32) NOT NULL,
    Checksum VARCHAR(64) NOT NULL,
    Dependencies VARCHAR(500),
    AppliedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (MigrationId)
)`

const defaultTemplate = `-- Migration: {{.Version}}
-- Dependencies: {{.Dependencies}}
-- Description: {{.Description}}

-- Up Migration
{{.UpSQL}}

-- Down Migration
{{.DownSQL}}
`

type MigrationTemplate struct {
	Version      string
	Dependencies string
	Description  string
	UpSQL        string
	DownSQL      string
}

type mysqlConn interface {
	Execute(query string, args ...interface{}) (*mysql.Result, error)
	Begin() error
	Commit() error
	Rollback() error
	Close() error
	Ping() error
	UseDB(dbName string) error
	GetDB() string
}

func handleTemplateCreation() error {
	if *templateName == "" {
		return nil
	}
	if err := createMigrationTemplate(*templateName); err != nil {
		return fmt.Errorf("error creating migration template: %v", err)
	}
	return nil
}

func handleValidation() error {
	if !*validate {
		return nil
	}
	if err := validateMigrations(); err != nil {
		return fmt.Errorf("error validating migrations: %v", err)
	}
	return nil
}

func handleMigrationOperations(conn mysqlConn) error {
	if *status {
		return showMigrationStatus(conn)
	}
	if *rollback != "" {
		return rollbackMigration(conn, *rollback)
	}
	if *file != "" && *version != "" {
		return applyMigration(conn, *version, *file)
	}
	if *file != "" {
		return executeFile(conn, *file)
	}
	return nil
}

func main() {
	pflag.Parse()

	if *templateName != "" {
		if err := handleTemplateCreation(); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating template: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var conn mysqlConn
	realConn, err := client.Connect(fmt.Sprintf("%s:%d", *host, *port), *user, *password, *database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to MySQL: %v\n", err)
		os.Exit(1)
	}
	conn = realConn
	defer conn.Close()

	if err := initMigrationsTable(conn); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing migrations table: %v\n", err)
		os.Exit(1)
	}

	if err := handleMigrationOperations(conn); err != nil {
		fmt.Fprintf(os.Stderr, "Error handling migration operations: %v\n", err)
		os.Exit(1)
	}

	if *interactive {
		runInteractive(conn)
	} else if len(pflag.Args()) > 0 {
		if err := executeQuery(conn, strings.Join(pflag.Args(), " ")); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing query: %v\n", err)
			os.Exit(1)
		}
	}
}

func createMigrationTemplate(templateName string) error {
	// Create migrations directory if it doesn't exist
	if err := os.MkdirAll("migrations", 0755); err != nil {
		return fmt.Errorf("error creating migrations directory: %v", err)
	}

	// Create template file
	templatePath := filepath.Join("migrations", templateName+".tmpl")
	if err := os.WriteFile(templatePath, []byte(defaultTemplate), 0644); err != nil {
		return fmt.Errorf("error creating template file: %v", err)
	}

	fmt.Printf("Created migration template: %s\n", templatePath)
	return nil
}

func validateMigrations() error {
	// Check for migration files
	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		return fmt.Errorf("error finding migration files: %v", err)
	}

	if len(files) == 0 {
		fmt.Println("No migration files found.")
		return nil
	}

	// Validate each migration file
	for _, file := range files {
		if err := validateMigrationFile(file); err != nil {
			fmt.Printf("Error validating %s: %v\n", file, err)
			continue
		}
		fmt.Printf("✓ %s\n", file)
	}

	return nil
}

func validateMigrationFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var currentQuery strings.Builder
	var hasUpMigration, hasDownMigration bool
	var inUpSection, inDownSection bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Check for section markers
		if strings.HasPrefix(line, "-- Up Migration") {
			inUpSection = true
			inDownSection = false
			continue
		}
		if strings.HasPrefix(line, "-- Down Migration") {
			inUpSection = false
			inDownSection = true
			continue
		}

		// Skip comments and empty lines
		if strings.HasPrefix(line, "--") || strings.HasPrefix(line, "/*") || line == "" {
			continue
		}

		currentQuery.WriteString(line)
		if strings.HasSuffix(line, ";") {
			query := currentQuery.String()
			if err := validateQuery(query); err != nil {
				return fmt.Errorf("invalid query: %v", err)
			}
			if inUpSection {
				hasUpMigration = true
			}
			if inDownSection {
				hasDownMigration = true
			}
			currentQuery.Reset()
		}
	}

	if !hasUpMigration {
		return fmt.Errorf("missing up migration")
	}
	if !hasDownMigration {
		return fmt.Errorf("missing down migration")
	}

	return nil
}

func validateQuery(query string) error {
	// Basic SQL validation
	query = strings.ToLower(query)
	if strings.Contains(query, "drop table") && !strings.Contains(query, "if exists") {
		return fmt.Errorf("DROP TABLE should use IF EXISTS")
	}
	if strings.Contains(query, "alter table") && !strings.Contains(query, "if exists") {
		return fmt.Errorf("ALTER TABLE should use IF EXISTS")
	}
	return nil
}

func calculateChecksum(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func checkDependencies(conn mysqlConn, dependencies []string) error {
	if len(dependencies) == 0 {
		return nil
	}

	// Clean dependencies
	cleanDeps := make([]string, 0, len(dependencies))
	for _, dep := range dependencies {
		dep = strings.TrimSpace(dep)
		if dep != "" {
			cleanDeps = append(cleanDeps, dep)
		}
	}

	if len(cleanDeps) == 0 {
		return nil
	}

	// Build query with placeholders
	placeholders := make([]string, len(cleanDeps))
	for i := range cleanDeps {
		placeholders[i] = "PARAM"
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM __EFMigrationsHistory WHERE MigrationId IN (%s)", strings.Join(placeholders, ","))

	// Convert dependencies to interface slice for query arguments
	args := make([]interface{}, len(cleanDeps))
	for i, dep := range cleanDeps {
		args[i] = dep
	}

	result, err := conn.Execute(query, args...)
	if err != nil {
		return fmt.Errorf("failed to check dependencies: %v", err)
	}

	if result.Resultset == nil || len(result.Resultset.Values) == 0 {
		return fmt.Errorf("missing dependencies: %v", cleanDeps)
	}

	countStr := string(result.Resultset.Values[0][0].AsString())
	count, err := strconv.ParseInt(countStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid count result: %v", err)
	}

	if count == 0 {
		return fmt.Errorf("missing dependencies: %v", cleanDeps)
	}

	return nil
}

func rollbackMigration(conn mysqlConn, targetVersion string) error {
	// Get current migrations
	result, err := conn.Execute("SELECT MigrationId FROM __EFMigrationsHistory ORDER BY AppliedAt DESC")
	if err != nil {
		return fmt.Errorf("error getting migrations: %v", err)
	}

	if result.Resultset == nil || len(result.Resultset.Values) == 0 {
		return fmt.Errorf("no migrations to roll back")
	}

	// Check if target version exists
	targetFound := false
	for _, row := range result.Resultset.Values {
		currentVersion := string(row[0].AsString())
		if currentVersion == targetVersion {
			targetFound = true
			break
		}
	}

	if !targetFound {
		return fmt.Errorf("target version %s not found", targetVersion)
	}

	// Start transaction
	if err := conn.Begin(); err != nil {
		return fmt.Errorf("error starting transaction: %v", err)
	}
	defer conn.Rollback()

	// Roll back migrations until we reach the target version
	for _, row := range result.Resultset.Values {
		currentVersion := string(row[0].AsString())
		if currentVersion == targetVersion {
			break
		}

		// Execute rollback SQL file
		rollbackFile := fmt.Sprintf("migrations/%s_rollback.sql", currentVersion)
		if err := executeFile(conn, rollbackFile); err != nil {
			return fmt.Errorf("error executing rollback file %s: %v", rollbackFile, err)
		}

		// Remove migration record
		if _, err := conn.Execute("DELETE FROM __EFMigrationsHistory WHERE MigrationId = PARAM", currentVersion); err != nil {
			return fmt.Errorf("error removing migration record: %v", err)
		}

		fmt.Printf("Successfully rolled back migration %s\n", currentVersion)
	}

	// Commit transaction
	if err := conn.Commit(); err != nil {
		return fmt.Errorf("error committing transaction: %v", err)
	}

	return nil
}

func showMigrationStatus(conn mysqlConn) error {
	result, err := conn.Execute(`
		SELECT MigrationId, ProductVersion, Checksum, Dependencies, AppliedAt 
		FROM __EFMigrationsHistory 
		ORDER BY AppliedAt`)
	if err != nil {
		return fmt.Errorf("error querying migration status: %v", err)
	}

	if result.Resultset == nil || len(result.Resultset.Values) == 0 {
		fmt.Println("No migrations have been applied.")
		return nil
	}

	fmt.Println("Applied Migrations:")
	fmt.Println("------------------")
	for _, row := range result.Resultset.Values {
		fmt.Printf("Migration: %s\nVersion: %s\nChecksum: %s\nDependencies: %s\nApplied: %s\n\n",
			string(row[0].AsString()),
			string(row[1].AsString()),
			string(row[2].AsString()),
			string(row[3].AsString()),
			string(row[4].AsString()))
	}
	return nil
}

func applyMigration(conn mysqlConn, version string, file string) error {
	// Check if migration already exists
	result, err := conn.Execute("SELECT MigrationId FROM __EFMigrationsHistory WHERE MigrationId = PARAM", version)
	if err != nil {
		return fmt.Errorf("error checking migration status: %v", err)
	}

	if result.Resultset != nil && len(result.Resultset.Values) > 0 {
		return fmt.Errorf("migration %s already exists", version)
	}

	// Check dependencies
	var dependencies []string
	if *depends != "" {
		dependencies = strings.Split(*depends, ",")
		if err := checkDependencies(conn, dependencies); err != nil {
			return fmt.Errorf("dependency check failed: %v", err)
		}
	}

	// Calculate checksum
	checksum, err := calculateChecksum(file)
	if err != nil {
		return fmt.Errorf("error calculating checksum: %v", err)
	}

	// Start transaction
	if err := conn.Begin(); err != nil {
		return fmt.Errorf("error starting transaction: %v", err)
	}
	defer conn.Rollback()

	// Execute migration file
	if err := executeFile(conn, file); err != nil {
		return fmt.Errorf("error executing migration file: %v", err)
	}

	// Insert migration record
	if _, err := conn.Execute("INSERT INTO __EFMigrationsHistory (MigrationId, ProductVersion, Checksum, Dependencies) VALUES (PARAM, PARAM, PARAM, PARAM)",
		version, "1.0.0", checksum, *depends); err != nil {
		return fmt.Errorf("error inserting migration record: %v", err)
	}

	// Commit transaction
	if err := conn.Commit(); err != nil {
		return fmt.Errorf("error committing transaction: %v", err)
	}

	fmt.Printf("Successfully applied migration %s\n", version)
	return nil
}

func initMigrationsTable(conn mysqlConn) error {
	_, err := conn.Execute(migrationsTable)
	return err
}

func executeFile(conn mysqlConn, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	// Start transaction
	if err := conn.Begin(); err != nil {
		return fmt.Errorf("error starting transaction: %v", err)
	}

	scanner := bufio.NewScanner(file)
	var currentQuery strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if strings.HasPrefix(line, "--") || strings.HasPrefix(line, "/*") || line == "" {
			continue
		}

		currentQuery.WriteString(line)
		if strings.HasSuffix(line, ";") {
			query := currentQuery.String()
			if err := executeQuery(conn, query); err != nil {
				// Rollback on error
				conn.Rollback()
				return err
			}
			currentQuery.Reset()
		}
	}

	// Commit transaction
	if err := conn.Commit(); err != nil {
		return fmt.Errorf("error committing transaction: %v", err)
	}

	return nil
}

func runInteractive(conn mysqlConn) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("mysql> ")
		query, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		query = strings.TrimSpace(query)
		if query == "exit" || query == "quit" {
			break
		}

		if query == "" {
			continue
		}

		if err := executeQuery(conn, query); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
	}
}

func executeQuery(conn mysqlConn, query string) error {
	result, err := conn.Execute(query)
	if err != nil {
		return fmt.Errorf("error executing query: %v", err)
	}

	if result == nil {
		return fmt.Errorf("no result returned")
	}

	if result.Resultset == nil {
		fmt.Printf("Query OK, %d rows affected\n", result.AffectedRows)
		return nil
	}

	// Print column headers
	if result.Fields != nil {
		for i, field := range result.Fields {
			if i > 0 {
				fmt.Print("\t")
			}
			fmt.Print(field.Name)
		}
		fmt.Println()
	}

	// Print rows
	for _, row := range result.Values {
		for i, col := range row {
			if i > 0 {
				fmt.Print("\t")
			}
			// Convert field value to string
			val := col.AsString()
			if len(val) == 0 {
				fmt.Print("NULL")
			} else {
				fmt.Print(string(val))
			}
		}
		fmt.Println()
	}

	fmt.Printf("%d rows in set\n", len(result.Values))
	return nil
}
