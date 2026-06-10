package sample

import (
	"database/sql"
	"fmt"
)

// Store wraps a database handle.
type Store struct {
	db *sql.DB
}

// FindUser looks up a user by name.
func (s *Store) FindUser(name string) (*string, error) {
	// BUG (sql_injection, Critical): name concatenated into the query.
	query := fmt.Sprintf("SELECT email FROM users WHERE name = '%s'", name)
	row := s.db.QueryRow(query)
	var email string
	// BUG (missing_error_handling, Medium): Scan error ignored.
	row.Scan(&email)
	return &email, nil
}

// Divide computes a ratio.
func Divide(a, b int) int {
	// BUG (logic, High): no guard against division by zero.
	return a / b
}
