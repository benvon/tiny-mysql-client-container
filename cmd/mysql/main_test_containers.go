package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/stretchr/testify/assert"
)

// MySQLConn implements the mysqlConn interface using database/sql
type MySQLConn struct {
	db *sql.DB
}

func (c *MySQLConn) Execute(query string, args ...interface{}) (*mysql.Result, error) {
	result, err := c.db.ExecContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}

	affectedRows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}

	return &mysql.Result{
		AffectedRows: uint64(affectedRows),
	}, nil
}

func (c *MySQLConn) Begin() error {
	_, err := c.db.ExecContext(context.Background(), "START TRANSACTION")
	return err
}

func (c *MySQLConn) Commit() error {
	_, err := c.db.ExecContext(context.Background(), "COMMIT")
	return err
}

func (c *MySQLConn) Rollback() error {
	_, err := c.db.ExecContext(context.Background(), "ROLLBACK")
	return err
}

func (c *MySQLConn) Close() error {
	return c.db.Close()
}

func (c *MySQLConn) Ping() error {
	return c.db.Ping()
}

func (c *MySQLConn) UseDB(dbName string) error {
	_, err := c.db.ExecContext(context.Background(), "USE "+dbName)
	return err
}

func (c *MySQLConn) GetDB() string {
	var dbName string
	err := c.db.QueryRowContext(context.Background(), "SELECT DATABASE()").Scan(&dbName)
	if err != nil {
		return ""
	}
	return dbName
}

func TestCalculateChecksumContainer(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	// Write some content
	content := "test content"
	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}

	// Calculate checksum
	checksum, err := calculateChecksum(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Verify checksum
	expectedHash := sha256.Sum256([]byte(content))
	expectedChecksum := hex.EncodeToString(expectedHash[:])
	if checksum != expectedChecksum {
		t.Errorf("Expected checksum %s, got %s", expectedChecksum, checksum)
	}
}

func TestValidateQueryContainer(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{
			name:    "Valid DROP TABLE",
			query:   "DROP TABLE IF EXISTS test;",
			wantErr: false,
		},
		{
			name:    "Invalid DROP TABLE",
			query:   "DROP TABLE test;",
			wantErr: true,
		},
		{
			name:    "Valid ALTER TABLE",
			query:   "ALTER TABLE IF EXISTS test ADD COLUMN x INT;",
			wantErr: false,
		},
		{
			name:    "Invalid ALTER TABLE",
			query:   "ALTER TABLE test ADD COLUMN x INT;",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQuery(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateQuery() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMigrationFileContainer(t *testing.T) {
	// Create a temporary directory for test files
	tmpdir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "Valid migration",
			content: `-- Up Migration
CREATE TABLE IF NOT EXISTS test (id INT);
-- Down Migration
DROP TABLE IF EXISTS test;`,
			wantErr: false,
		},
		{
			name: "Missing up migration",
			content: `-- Down Migration
DROP TABLE IF EXISTS test;`,
			wantErr: true,
		},
		{
			name: "Missing down migration",
			content: `-- Up Migration
CREATE TABLE IF NOT EXISTS test (id INT);`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpfile, err := os.CreateTemp(tmpdir, "test*.sql")
			if err != nil {
				t.Fatal(err)
			}

			if _, err := tmpfile.Write([]byte(tt.content)); err != nil {
				t.Fatal(err)
			}

			err = validateMigrationFile(tmpfile.Name())
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMigrationFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCreateMigrationTemplateContainer(t *testing.T) {
	// Create a temporary directory for test files
	tmpdir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	// Change to temporary directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	if err := os.Chdir(tmpdir); err != nil {
		t.Fatal(err)
	}

	// Test template creation
	templateName := "test_template"
	if err := createMigrationTemplate(templateName); err != nil {
		t.Fatal(err)
	}

	// Verify template file exists
	templatePath := filepath.Join("migrations", templateName+".tmpl")
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		t.Error("Template file was not created")
	}

	// Verify template content
	content, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatal(err)
	}

	if string(content) != defaultTemplate {
		t.Error("Template content does not match expected content")
	}
}

func TestCheckDependenciesContainer(t *testing.T) {
	tests := []struct {
		name         string
		dependencies []string
		setupDB      func(*sql.DB) error
		wantErr      bool
	}{
		{
			name:         "No dependencies",
			dependencies: []string{},
			setupDB:      func(db *sql.DB) error { return nil },
			wantErr:      false,
		},
		{
			name:         "All dependencies exist",
			dependencies: []string{"20240314000000_InitialCreate"},
			setupDB: func(db *sql.DB) error {
				_, err := db.ExecContext(context.Background(), `
					CREATE TABLE IF NOT EXISTS __EFMigrationsHistory (
						MigrationId VARCHAR(255) PRIMARY KEY,
						ProductVersion VARCHAR(255),
						Checksum VARCHAR(255),
						Dependencies TEXT,
						AppliedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP
					)
				`)
				if err != nil {
					return err
				}
				_, err = db.ExecContext(context.Background(), `
					INSERT INTO __EFMigrationsHistory (MigrationId, ProductVersion, Checksum, Dependencies)
					VALUES ('20240314000000_InitialCreate', '1.0.0', 'test', '')
				`)
				return err
			},
			wantErr: false,
		},
		{
			name:         "Missing dependencies",
			dependencies: []string{"20240314000000_InitialCreate"},
			setupDB:      func(db *sql.DB) error { return nil },
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			WithTestMySQLContainer(t, func(container *TestMySQLContainer) {
				if tt.setupDB != nil {
					err := tt.setupDB(container.DB)
					if err != nil {
						t.Fatalf("Failed to setup database: %v", err)
					}
				}

				conn := &MySQLConn{db: container.DB}
				err := checkDependencies(conn, tt.dependencies)
				if tt.wantErr {
					assert.Error(t, err)
					return
				}
				assert.NoError(t, err)
			})
		})
	}
}

func TestRollbackMigrationContainer(t *testing.T) {
	// Create test SQL file
	testSQL := "CREATE TABLE test (id INT PRIMARY KEY);"
	err := os.MkdirAll("testdata", 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile("testdata/test.sql", []byte(testSQL), 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll("testdata")

	// Create migrations directory
	err = os.MkdirAll("migrations", 0755)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll("migrations")

	tests := []struct {
		name    string
		version string
		setupDB func(*sql.DB) error
		wantErr bool
		checkDB func(*sql.DB) error
	}{
		{
			name:    "Successful rollback",
			version: "20240314000000_InitialCreate",
			setupDB: func(db *sql.DB) error {
				// Create migrations history table
				_, err := db.ExecContext(context.Background(), `
					CREATE TABLE IF NOT EXISTS __EFMigrationsHistory (
						MigrationId VARCHAR(255) PRIMARY KEY,
						ProductVersion VARCHAR(255),
						Checksum VARCHAR(255),
						Dependencies TEXT,
						AppliedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP
					)
				`)
				if err != nil {
					return err
				}

				// Insert test migration
				_, err = db.ExecContext(context.Background(), `
					INSERT INTO __EFMigrationsHistory (MigrationId, ProductVersion, Checksum, Dependencies)
					VALUES ('20240314000000_InitialCreate', '1.0.0', 'test', '')
				`)
				if err != nil {
					return err
				}

				// Create test table
				_, err = db.ExecContext(context.Background(), testSQL)
				return err
			},
			wantErr: false,
			checkDB: func(db *sql.DB) error {
				// Check if migration was removed from history
				var count int
				err := db.QueryRowContext(context.Background(), `
					SELECT COUNT(*) FROM __EFMigrationsHistory 
					WHERE MigrationId = '20240314000000_InitialCreate'
				`).Scan(&count)
				if err != nil {
					return err
				}
				if count != 0 {
					return fmt.Errorf("expected migration to be removed from history")
				}

				// Check if table was dropped
				err = db.QueryRowContext(context.Background(), `
					SELECT COUNT(*) FROM information_schema.tables 
					WHERE table_name = 'test'
				`).Scan(&count)
				if err != nil {
					return err
				}
				if count != 0 {
					return fmt.Errorf("expected table to be dropped")
				}

				return nil
			},
		},
		{
			name:    "No migrations to rollback",
			version: "20240314000000_InitialCreate",
			setupDB: func(db *sql.DB) error {
				// Create migrations history table
				_, err := db.ExecContext(context.Background(), `
					CREATE TABLE IF NOT EXISTS __EFMigrationsHistory (
						MigrationId VARCHAR(255) PRIMARY KEY,
						ProductVersion VARCHAR(255),
						Checksum VARCHAR(255),
						Dependencies TEXT,
						AppliedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP
					)
				`)
				return err
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			WithTestMySQLContainer(t, func(container *TestMySQLContainer) {
				if tt.setupDB != nil {
					err := tt.setupDB(container.DB)
					if err != nil {
						t.Fatalf("Failed to setup database: %v", err)
					}
				}

				conn := &MySQLConn{db: container.DB}
				err := rollbackMigration(conn, tt.version)
				if tt.wantErr {
					assert.Error(t, err)
					return
				}
				assert.NoError(t, err)

				if tt.checkDB != nil {
					err := tt.checkDB(container.DB)
					if err != nil {
						t.Errorf("Database check failed: %v", err)
					}
				}
			})
		})
	}
}

func TestShowMigrationStatusContainer(t *testing.T) {
	tests := []struct {
		name    string
		setupDB func(*sql.DB) error
		wantErr bool
	}{
		{
			name: "Show migrations",
			setupDB: func(db *sql.DB) error {
				// Create migrations history table
				_, err := db.ExecContext(context.Background(), `
					CREATE TABLE IF NOT EXISTS __EFMigrationsHistory (
						MigrationId VARCHAR(255) PRIMARY KEY,
						ProductVersion VARCHAR(255),
						Checksum VARCHAR(255),
						Dependencies TEXT,
						AppliedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP
					)
				`)
				if err != nil {
					return err
				}

				// Insert test migrations
				_, err = db.ExecContext(context.Background(), `
					INSERT INTO __EFMigrationsHistory (MigrationId, ProductVersion, Checksum, Dependencies)
					VALUES 
						('20240314000000_InitialCreate', '1.0.0', 'test1', ''),
						('20240315000000_AddUsers', '1.0.0', 'test2', '20240314000000_InitialCreate')
				`)
				return err
			},
			wantErr: false,
		},
		{
			name: "No migrations",
			setupDB: func(db *sql.DB) error {
				// Create migrations history table
				_, err := db.ExecContext(context.Background(), `
					CREATE TABLE IF NOT EXISTS __EFMigrationsHistory (
						MigrationId VARCHAR(255) PRIMARY KEY,
						ProductVersion VARCHAR(255),
						Checksum VARCHAR(255),
						Dependencies TEXT,
						AppliedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP
					)
				`)
				return err
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			WithTestMySQLContainer(t, func(container *TestMySQLContainer) {
				if tt.setupDB != nil {
					err := tt.setupDB(container.DB)
					if err != nil {
						t.Fatalf("Failed to setup database: %v", err)
					}
				}

				conn := &MySQLConn{db: container.DB}
				err := showMigrationStatus(conn)
				if tt.wantErr {
					assert.Error(t, err)
					return
				}
				assert.NoError(t, err)
			})
		})
	}
}

func TestApplyMigrationContainer(t *testing.T) {
	tests := []struct {
		name    string
		version string
		setupDB func(*sql.DB) error
		wantErr bool
		checkDB func(*sql.DB) error
	}{
		{
			name:    "Migration already applied",
			version: "20240314000000_InitialCreate",
			setupDB: func(db *sql.DB) error {
				// Create migrations history table
				_, err := db.ExecContext(context.Background(), `
					CREATE TABLE IF NOT EXISTS __EFMigrationsHistory (
						MigrationId VARCHAR(255) PRIMARY KEY,
						ProductVersion VARCHAR(255),
						Checksum VARCHAR(255),
						Dependencies TEXT,
						AppliedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP
					)
				`)
				if err != nil {
					return err
				}

				// Insert test migration
				_, err = db.ExecContext(context.Background(), `
					INSERT INTO __EFMigrationsHistory (MigrationId, ProductVersion, Checksum, Dependencies)
					VALUES ('20240314000000_InitialCreate', '1.0.0', 'test', '')
				`)
				return err
			},
			wantErr: true,
		},
		{
			name:    "New migration",
			version: "20240314000000_InitialCreate",
			setupDB: func(db *sql.DB) error {
				// Create migrations history table
				_, err := db.ExecContext(context.Background(), `
					CREATE TABLE IF NOT EXISTS __EFMigrationsHistory (
						MigrationId VARCHAR(255) PRIMARY KEY,
						ProductVersion VARCHAR(255),
						Checksum VARCHAR(255),
						Dependencies TEXT,
						AppliedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP
					)
				`)
				return err
			},
			wantErr: false,
			checkDB: func(db *sql.DB) error {
				// Check if migration was added to history
				var count int
				err := db.QueryRowContext(context.Background(), `
					SELECT COUNT(*) FROM __EFMigrationsHistory 
					WHERE MigrationId = '20240314000000_InitialCreate'
				`).Scan(&count)
				if err != nil {
					return err
				}
				if count != 1 {
					return fmt.Errorf("expected migration to be added to history")
				}

				// Check if table was created
				err = db.QueryRowContext(context.Background(), `
					SELECT COUNT(*) FROM information_schema.tables 
					WHERE table_name = 'test'
				`).Scan(&count)
				if err != nil {
					return err
				}
				if count != 1 {
					return fmt.Errorf("expected table to be created")
				}

				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			WithTestMySQLContainer(t, func(container *TestMySQLContainer) {
				if tt.setupDB != nil {
					err := tt.setupDB(container.DB)
					if err != nil {
						t.Fatalf("Failed to setup database: %v", err)
					}
				}

				conn := &MySQLConn{db: container.DB}
				err := applyMigration(conn, tt.version, "testdata/test.sql")
				if tt.wantErr {
					assert.Error(t, err)
					return
				}
				assert.NoError(t, err)

				if tt.checkDB != nil {
					err := tt.checkDB(container.DB)
					if err != nil {
						t.Errorf("Database check failed: %v", err)
					}
				}
			})
		})
	}
}

func TestExecuteFileContainer(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "Valid SQL file",
			content: `CREATE TABLE test (
				id INT PRIMARY KEY
			);`,
			wantErr: false,
		},
		{
			name: "Invalid SQL file",
			content: `CREATE TABLE test (
				id INT PRIMARY KEY
			);`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			WithTestMySQLContainer(t, func(container *TestMySQLContainer) {
				// Create test file
				tmpfile, err := os.CreateTemp("", "test*.sql")
				if err != nil {
					t.Fatal(err)
				}
				defer os.Remove(tmpfile.Name())

				if _, err := tmpfile.Write([]byte(tt.content)); err != nil {
					t.Fatal(err)
				}

				conn := &MySQLConn{db: container.DB}
				err = executeFile(conn, tmpfile.Name())
				if tt.wantErr {
					assert.Error(t, err)
					return
				}
				assert.NoError(t, err)

				// Verify table was created
				var count int
				err = container.DB.QueryRowContext(context.Background(), `
					SELECT COUNT(*) FROM information_schema.tables 
					WHERE table_name = 'test'
				`).Scan(&count)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 {
					t.Error("Expected table to be created")
				}
			})
		})
	}
}

func TestExecuteQueryContainer(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{
			name:    "Valid query",
			query:   "SELECT 1",
			wantErr: false,
		},
		{
			name:    "Invalid query",
			query:   "SELECT * FROM nonexistent_table",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			WithTestMySQLContainer(t, func(container *TestMySQLContainer) {
				conn := &MySQLConn{db: container.DB}
				err := executeQuery(conn, tt.query)
				if tt.wantErr {
					assert.Error(t, err)
					return
				}
				assert.NoError(t, err)
			})
		})
	}
}

func TestHandleTemplateCreationContainer(t *testing.T) {
	tests := []struct {
		name     string
		template string
		wantErr  bool
	}{
		{
			name:     "No template specified",
			template: "",
			wantErr:  true,
		},
		{
			name:     "Create template",
			template: "test_template",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for test files
			tmpdir, err := os.MkdirTemp("", "test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tmpdir)

			// Change to temporary directory
			oldDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(oldDir)
			if err := os.Chdir(tmpdir); err != nil {
				t.Fatal(err)
			}

			*templateName = tt.template
			err = handleTemplateCreation()
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Verify template file exists
			templatePath := filepath.Join("migrations", tt.template+".tmpl")
			if _, err := os.Stat(templatePath); os.IsNotExist(err) {
				t.Error("Template file was not created")
			}

			// Verify template content
			content, err := os.ReadFile(templatePath)
			if err != nil {
				t.Fatal(err)
			}

			if string(content) != defaultTemplate {
				t.Error("Template content does not match expected content")
			}
		})
	}
}

func TestHandleValidationContainer(t *testing.T) {
	tests := []struct {
		name     string
		validate bool
		wantErr  bool
	}{
		{
			name:     "Validation disabled",
			validate: false,
			wantErr:  false,
		},
		{
			name:     "Validation enabled",
			validate: true,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*validate = tt.validate
			err := handleValidation()
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestHandleMigrationOperationsContainer(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		setupDB   func(*sql.DB) error
		wantErr   bool
	}{
		{
			name:      "Show status",
			operation: "status",
			setupDB: func(db *sql.DB) error {
				// Create migrations history table
				_, err := db.ExecContext(context.Background(), `
					CREATE TABLE IF NOT EXISTS __EFMigrationsHistory (
						MigrationId VARCHAR(255) PRIMARY KEY,
						ProductVersion VARCHAR(255),
						Checksum VARCHAR(255),
						Dependencies TEXT,
						AppliedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP
					)
				`)
				return err
			},
			wantErr: false,
		},
		{
			name:      "Rollback migration",
			operation: "rollback",
			setupDB: func(db *sql.DB) error {
				// Create migrations history table
				_, err := db.ExecContext(context.Background(), `
					CREATE TABLE IF NOT EXISTS __EFMigrationsHistory (
						MigrationId VARCHAR(255) PRIMARY KEY,
						ProductVersion VARCHAR(255),
						Checksum VARCHAR(255),
						Dependencies TEXT,
						AppliedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP
					)
				`)
				if err != nil {
					return err
				}

				// Insert test migration
				_, err = db.ExecContext(context.Background(), `
					INSERT INTO __EFMigrationsHistory (MigrationId, ProductVersion, Checksum, Dependencies)
					VALUES ('20240314000000_InitialCreate', '1.0.0', 'test', '')
				`)
				return err
			},
			wantErr: true,
		},
		{
			name:      "Apply migration",
			operation: "apply",
			setupDB: func(db *sql.DB) error {
				// Create migrations history table
				_, err := db.ExecContext(context.Background(), `
					CREATE TABLE IF NOT EXISTS __EFMigrationsHistory (
						MigrationId VARCHAR(255) PRIMARY KEY,
						ProductVersion VARCHAR(255),
						Checksum VARCHAR(255),
						Dependencies TEXT,
						AppliedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP
					)
				`)
				return err
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			WithTestMySQLContainer(t, func(container *TestMySQLContainer) {
				if tt.setupDB != nil {
					err := tt.setupDB(container.DB)
					if err != nil {
						t.Fatalf("Failed to setup database: %v", err)
					}
				}

				conn := &MySQLConn{db: container.DB}
				err := handleMigrationOperations(conn)
				if tt.wantErr {
					assert.Error(t, err)
					return
				}
				assert.NoError(t, err)
			})
		})
	}
}
