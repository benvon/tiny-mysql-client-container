package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/go-mysql-org/go-mysql/client"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/spf13/pflag"
)

// Define application version
const appVersion = "dev"

var (
	host        = pflag.StringP("host", "h", "localhost", "MySQL host")
	port        = pflag.Int32P("port", "p", 3306, "MySQL port")
	user        = pflag.StringP("user", "u", "root", "MySQL user")
	password    = pflag.StringP("password", "P", "", "MySQL password")
	database    = pflag.StringP("database", "d", "", "MySQL database")
	file        = pflag.StringP("file", "f", "", "SQL file to execute")
	showVersion = pflag.BoolP("version", "v", false, "Show application version") // Renamed and repurposed
	interactive = pflag.BoolP("interactive", "i", false, "Run in interactive mode")
	// Removed: status, rollback, validate, templateName, interactive, depends, dryRun
)

type mysqlConn interface {
	Execute(query string, args ...interface{}) (*mysql.Result, error)
	// Removed: Begin, Commit, Rollback
	Close() error
	Ping() error
	UseDB(dbName string) error
	GetDB() string
}

func main() {
	pflag.Parse()

	// Handle version flag first
	if *showVersion {
		fmt.Println("tiny-mysql-client version:", appVersion)
		os.Exit(0)
	}

	// Check if any execution mode is specified
	hasArgs := len(pflag.Args()) > 0
	if *file == "" && !*interactive && !hasArgs {
		fmt.Fprintln(os.Stderr, "Error: No query, file, or interactive mode specified.")
		pflag.Usage()
		os.Exit(1)
	}

	// Establish connection
	conn, err := client.Connect(fmt.Sprintf("%s:%d", *host, *port), *user, *password, *database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to MySQL: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Execute based on flags/args
	if *file != "" {
		if err := executeFile(conn, *file); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing file %s: %v\n", *file, err)
			os.Exit(1)
		}
	} else if *interactive {
		runInteractive(conn)
	} else if hasArgs {
		query := strings.Join(pflag.Args(), " ")
		if err := executeQuery(conn, query); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing query: %v\n", err)
			os.Exit(1)
		}
	}
}

func executeFile(conn mysqlConn, filename string) error {
	fileHandle, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("error opening file: %v", err)
	}
	defer fileHandle.Close()

	scanner := bufio.NewScanner(fileHandle)
	var currentQuery strings.Builder
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)

		// Skip comments and empty lines
		if strings.HasPrefix(trimmedLine, "--") || strings.HasPrefix(trimmedLine, "/*") || trimmedLine == "" {
			continue
		}

		currentQuery.WriteString(line)
		currentQuery.WriteString("\n") // Preserve original line breaks somewhat

		if strings.HasSuffix(trimmedLine, ";") {
			query := strings.TrimSpace(currentQuery.String())
			if query != "" {
				fmt.Printf("Executing query from line %d:\n%s\n", lineNumber, query) // Added line number context
				if err := executeQuery(conn, query); err != nil {
					// Report error but continue processing the rest of the file?
					// Or return immediately? Let's return immediately for now.
					return fmt.Errorf("error on line %d: %v", lineNumber, err)
				}
				fmt.Println("---") // Separator between query results
			}
			currentQuery.Reset()
		}
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading file: %v", err)
	}

	// Execute any remaining query part (if file doesn't end with ';')
	finalQuery := strings.TrimSpace(currentQuery.String())
	if finalQuery != "" {
		fmt.Printf("Executing final query part from line %d:\n%s\n", lineNumber, finalQuery)
		if err := executeQuery(conn, finalQuery); err != nil {
			return fmt.Errorf("error on final part (line %d): %v", lineNumber, err)
		}
	}

	return nil
}

func runInteractive(conn mysqlConn) {
	reader := bufio.NewReader(os.Stdin)
	currentDb := conn.GetDB()
	prompt := fmt.Sprintf("mysql (%s)> ", currentDb)

	for {
		fmt.Print(prompt)
		input, err := reader.ReadString('\n')
		if err != nil {
			// Handle EOF (Ctrl+D) gracefully
			if err.Error() == "EOF" {
				fmt.Println("\nBye")
				break
			}
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			break // Exit on other errors
		}

		query := strings.TrimSpace(input)
		lowerQuery := strings.ToLower(query)

		if lowerQuery == "exit" || lowerQuery == "quit" {
			break
		}

		if query == "" {
			continue
		}

		// Handle USE DATABASE command to update prompt
		if strings.HasPrefix(lowerQuery, "use ") {
			dbName := strings.TrimSpace(strings.TrimPrefix(lowerQuery, "use "))
			if dbName != "" && !strings.HasSuffix(dbName, ";") { // Basic check, might need improvement
				dbName = strings.TrimSuffix(dbName, ";")
				if err := conn.UseDB(dbName); err != nil {
					fmt.Fprintf(os.Stderr, "Error changing database: %v\n", err)
				} else {
					currentDb = conn.GetDB() // Update prompt
					prompt = fmt.Sprintf("mysql (%s)> ", currentDb)
					fmt.Println("Database changed")
				}
				continue // Skip executing USE as a general query via executeQuery
			}
		}

		if err := executeQuery(conn, query); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
	}
}

func executeQuery(conn mysqlConn, query string) error {
	// Basic check to prevent executing empty queries (e.g., just ";")
	if strings.TrimSpace(query) == "" || strings.TrimSpace(query) == ";" {
		return nil // Or return an error? Let's treat as no-op.
	}

	result, err := conn.Execute(query)
	if err != nil {
		// Don't wrap the error here, let the caller handle context
		return err
	}

	// Handle results (remains the same as before)
	if result == nil {
		// This case should ideally not happen with go-mysql, but good practice
		fmt.Println("Query executed, no result returned.")
		return nil
	}

	if result.Resultset == nil {
		// This means it was an INSERT, UPDATE, DELETE, CREATE, etc.
		fmt.Printf("Query OK, %d rows affected\n", result.AffectedRows)
		if result.InsertId > 0 {
			fmt.Printf("Insert ID: %d\n", result.InsertId)
		}
		return nil
	}

	// This means it was a SELECT or SHOW statement
	// Print column headers
	if len(result.Fields) > 0 {
		header := make([]string, len(result.Fields))
		for i, field := range result.Fields {
			header[i] = string(field.Name)
		}
		fmt.Println(strings.Join(header, "\t"))
		fmt.Println(strings.Repeat("-", len(strings.Join(header, "\t")))) // Separator line
	}

	// Print rows
	if len(result.Values) > 0 {
		for _, row := range result.Values {
			rowStr := make([]string, len(row))
			for i, col := range row {
				// Check for nil value directly
				if col.Value() == nil {
					rowStr[i] = "NULL"
				} else {
					// Convert AsString() result (byte slice) to string
					rowStr[i] = string(col.AsString())
				}
			}
			fmt.Println(strings.Join(rowStr, "\t"))
		}
		fmt.Printf("\n%d row(s) in set\n", len(result.Values))
	} else {
		fmt.Println("Empty set") // Indicate when a SELECT returns no rows
	}

	return nil
}
