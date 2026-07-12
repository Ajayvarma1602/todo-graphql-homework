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

// ErrNotFound is returned when a lookup by ID matches no row
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

// UserByID returns a single user, or ErrNotFound if the id doesn't exist.
func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, name FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query user %d: %w", id, err)
	}
	return &u, nil
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


// TaskByID returns a single task (with tags) or ErrNotFound.
func (s *Store) TaskByID(ctx context.Context, id int64) (*Task, error) {
	var t Task
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, title, description, status, due_date FROM tasks WHERE id = $1`, id,
	).Scan(&t.ID, &t.UserID, &t.Title, &t.Description, &t.Status, &t.DueDate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query task %d: %w", id, err)
	}
	one := []Task{t}
	if err := s.attachTags(ctx, one); err != nil {
		return nil, err
	}
	return &one[0], nil
}

// NewTask carries the inputs for CreateTask.
type NewTask struct {
	UserID      int64
	Title       string
	Description *string
	DueDate     *time.Time
	Tags        []string
}

// CreateTask inserts a task and its tags in one transaction and returns it.
func (s *Store) CreateTask(ctx context.Context, in NewTask) (*Task, error) {
	// Check the user exists first so the caller gets a clear error
	// instead of a raw foreign-key violation.
	if _, err := s.UserByID(ctx, in.UserID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("user %d: %w", in.UserID, ErrNotFound)
		}
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var id int64
	// status is intentionally omitted: the column has DEFAULT 'pending', which
	// is the correct initial state for a freshly created task.
	err = tx.QueryRowContext(ctx,
		`INSERT INTO tasks (user_id, title, description, due_date)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		in.UserID, in.Title, in.Description, in.DueDate,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("insert task: %w", err)
	}

	seen := map[string]bool{}
	for _, tag := range in.Tags {
		if tag == "" || seen[tag] {
			continue // skip empties and duplicates (task_tags PK is (task_id, tag))
		}
		seen[tag] = true
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO task_tags (task_id, tag) VALUES ($1, $2)`, id, tag,
		); err != nil {
			return nil, fmt.Errorf("insert tag %q: %w", tag, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return s.TaskByID(ctx, id)
}

// UpdateTaskStatus sets a task's status (and bumps updated_at); ErrNotFound if missing.
func (s *Store) UpdateTaskStatus(ctx context.Context, id int64, status string) (*Task, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status = $1, updated_at = now() WHERE id = $2`, status, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update task %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.TaskByID(ctx, id)
}
