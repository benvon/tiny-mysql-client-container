package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMySQLContainerSetup(t *testing.T) {
	WithTestMySQLContainer(t, func(container *TestMySQLContainer) {
		// Test database connection
		err := container.DB.Ping()
		assert.NoError(t, err)

		// Create a test table
		_, err = container.DB.ExecContext(context.Background(), `
			CREATE TABLE test (
				id INT PRIMARY KEY,
				name VARCHAR(255)
			)
		`)
		assert.NoError(t, err)

		// Insert test data
		_, err = container.DB.ExecContext(context.Background(), `
			INSERT INTO test (id, name) VALUES (1, 'test1'), (2, 'test2')
		`)
		assert.NoError(t, err)

		// Query test data
		var count int
		err = container.DB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test").Scan(&count)
		assert.NoError(t, err)
		assert.Equal(t, 2, count)

		// Test transaction
		tx, err := container.DB.BeginTx(context.Background(), nil)
		assert.NoError(t, err)

		_, err = tx.ExecContext(context.Background(), "INSERT INTO test (id, name) VALUES (3, 'test3')")
		assert.NoError(t, err)

		err = tx.Commit()
		assert.NoError(t, err)

		// Verify transaction
		err = container.DB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test").Scan(&count)
		assert.NoError(t, err)
		assert.Equal(t, 3, count)
	})
}
