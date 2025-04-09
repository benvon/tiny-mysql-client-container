package main

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestMySQLContainer represents a MySQL container for testing
type TestMySQLContainer struct {
	Container testcontainers.Container
	DB        *sql.DB
}

// NewTestMySQLContainer creates and starts a new MySQL container for testing
func NewTestMySQLContainer(t *testing.T) (*TestMySQLContainer, error) {
	ctx := context.Background()

	// Define container request
	req := testcontainers.ContainerRequest{
		Image:        "mysql:8.0",
		ExposedPorts: []string{"3306/tcp"},
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD": "test",
			"MYSQL_DATABASE":      "testdb",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("port: 3306  MySQL Community Server"),
			wait.ForSQL("3306/tcp", "mysql", func(host string, port nat.Port) string {
				return fmt.Sprintf("root:test@tcp(%s:%s)/testdb", host, port.Port())
			}),
		).WithStartupTimeout(120 * time.Second),
	}

	// Start container
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start container: %v", err)
	}

	// Get container host and port
	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "3306")
	if err != nil {
		return nil, fmt.Errorf("failed to get container port: %v", err)
	}

	// Wait a bit for MySQL to be fully ready
	time.Sleep(5 * time.Second)

	// Create database connection with retry logic
	var db *sql.DB
	for i := 0; i < 5; i++ {
		dsn := fmt.Sprintf("root:test@tcp(%s:%s)/testdb?parseTime=true", host, port.Port())
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		// Test connection
		if err := db.Ping(); err != nil {
			db.Close()
			time.Sleep(2 * time.Second)
			continue
		}

		// Connection successful
		break
	}

	if db == nil || err != nil {
		return nil, fmt.Errorf("failed to connect to database after retries: %v", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &TestMySQLContainer{
		Container: container,
		DB:        db,
	}, nil
}

// Close stops and removes the container
func (c *TestMySQLContainer) Close(ctx context.Context) error {
	if c.DB != nil {
		if err := c.DB.Close(); err != nil {
			return fmt.Errorf("failed to close database connection: %v", err)
		}
	}
	if c.Container != nil {
		if err := c.Container.Terminate(ctx); err != nil {
			return fmt.Errorf("failed to terminate container: %v", err)
		}
	}
	return nil
}

// WithTestMySQLContainer is a helper function that runs tests with a MySQL container
func WithTestMySQLContainer(t *testing.T, fn func(*TestMySQLContainer)) {
	ctx := context.Background()
	container, err := NewTestMySQLContainer(t)
	if err != nil {
		t.Fatalf("Failed to create MySQL container: %v", err)
	}
	defer container.Close(ctx)

	fn(container)
}
