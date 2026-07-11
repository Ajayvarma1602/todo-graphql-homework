// Package store is the data access layer. All SQL lives here so the
// GraphQL resolvers never touch query strings directly.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound is returned when a lookup by ID matches no row.
var ErrNotFound = errors.New("not found")

// User mirrors a row of the users table.
type User struct {
	ID    int64
	Email string
	Name  string
}

// Task mirrors a row of the tasks table.
type Task struct {
	ID          int64
	UserID      int64
	Title       string
	Description *string    // nullable column → pointer; nil means NULL
	Status      string     // "pending" | "in_progress" | "done"
	DueDate     *time.Time // nullable column → pointer
	Tags        []string
}

// Store wraps the database connection with the queries the API needs.
type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store { return &Store{db: db} }

// Users returns all users ordered by id.
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email, name FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}


// TaskFilter narrows the Tasks query. Nil fields mean "no filter".
type TaskFilter struct {
	Status *string // DB values: "pending", "in_progress", "done"
	UserID *int64
}

// Tasks returns tasks matching the filter, ordered by id, with tags loaded.
func (s *Store) Tasks(ctx context.Context, f TaskFilter) ([]Task, error) {
	var (
		conds []string
		args  []any
	)
	if f.Status != nil {
		args = append(args, *f.Status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if f.UserID != nil {
		args = append(args, *f.UserID)
		conds = append(conds, fmt.Sprintf("user_id = $%d", len(args)))
	}

	q := `SELECT id, user_id, title, description, status, due_date FROM tasks`
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY id"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.UserID, &t.Title, &t.Description, &t.Status, &t.DueDate); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachTags(ctx, tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// attachTags loads tags for all given tasks in ONE query (avoids N+1).
func (s *Store) attachTags(ctx context.Context, tasks []Task) error {
	if len(tasks) == 0 {
		return nil
	}
	idx := make(map[int64]*Task, len(tasks))
	ids := make([]string, 0, len(tasks))
	for i := range tasks {
		tasks[i].Tags = []string{} // never nil: GraphQL type will be [String!]!
		idx[tasks[i].ID] = &tasks[i]
		ids = append(ids, fmt.Sprintf("%d", tasks[i].ID))
	}

	// Safe to join into SQL: these are int64s we formatted ourselves.
	q := fmt.Sprintf(
		`SELECT task_id, tag FROM task_tags WHERE task_id IN (%s) ORDER BY task_id, tag`,
		strings.Join(ids, ","),
	)
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			taskID int64
			tag    string
		)
		if err := rows.Scan(&taskID, &tag); err != nil {
			return fmt.Errorf("scan tag: %w", err)
		}
		if t, ok := idx[taskID]; ok {
			t.Tags = append(t.Tags, tag)
		}
	}
	return rows.Err()
}