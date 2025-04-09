package main

import (
	"context"
	"database/sql"
	"os"
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

func TestExecuteFileContainer(t *testing.T) {
	db, err := sql.Open("mysql", "root:password@tcp(127.0.0.1:3306)/testdb")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	conn := &MySQLConn{db: db}

	// Create a temporary SQL file
	tmpfile, err := os.CreateTemp("", "test*.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	content := `
CREATE TABLE IF NOT EXISTS test_file (id INT PRIMARY KEY);
INSERT INTO test_file (id) VALUES (1);
INSERT INTO test_file (id) VALUES (2);
`
	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close() // Close the file before executing

	if err := executeFile(conn, tmpfile.Name()); err != nil {
		t.Fatalf("executeFile() failed: %v", err)
	}

	// Verify data
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM test_file").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 2, count, "Expected 2 rows in test_file table")

	// Cleanup table (optional)
	_, err = db.Exec("DROP TABLE test_file")
	if err != nil {
		t.Logf("Warning: failed to drop test_file table: %v", err)
	}
}

func TestExecuteQueryContainer(t *testing.T) {
	db, err := sql.Open("mysql", "root:password@tcp(127.0.0.1:3306)/testdb")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	conn := &MySQLConn{db: db}

	// Setup: Create a test table
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS test_query (msg VARCHAR(255))")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE test_query")

	// Test INSERT
	insertQuery := "INSERT INTO test_query (msg) VALUES ('hello')"
	if err := executeQuery(conn, insertQuery); err != nil {
		t.Fatalf("executeQuery() failed for INSERT: %v", err)
	}

	// Test SELECT
	selectQuery := "SELECT msg FROM test_query WHERE msg = 'hello'"
	// We can't easily capture stdout here, so we just check for errors
	if err := executeQuery(conn, selectQuery); err != nil {
		t.Fatalf("executeQuery() failed for SELECT: %v", err)
	}
}
