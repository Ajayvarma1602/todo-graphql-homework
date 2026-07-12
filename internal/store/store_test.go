package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const testDSN = "postgres://admin:todo@localhost:5432/homework?sslmode=disable"

// newTestStore opens a store against the local Postgres from docker-compose.
// If the database isn't reachable (e.g. running `go test` without the
// container up), the test is skipped rather than failed — these are
// integration tests that need a live, seeded DB.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = testDSN
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("postgres not reachable (%v); run `docker compose up -d && go run ./cmd/seed` first", err)
	}
	return New(db)
}

func TestUsers(t *testing.T) {
	s := newTestStore(t)

	users, err := s.Users(context.Background())
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) == 0 {
		t.Fatal("expected seeded users, got none; run `go run ./cmd/seed`")
	}

	for _, u := range users {
		if u.ID == 0 || u.Email == "" || u.Name == "" {
			t.Errorf("user has empty required field: %+v", u)
		}
	}
}

func TestUserByID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	users, err := s.Users(ctx)
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) == 0 {
		t.Skip("no seeded users to look up")
	}

	want := users[0]
	got, err := s.UserByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("UserByID(%d): %v", want.ID, err)
	}
	if *got != want {
		t.Errorf("UserByID(%d) = %+v, want %+v", want.ID, *got, want)
	}

	if _, err := s.UserByID(ctx, 1<<62); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByID(missing) error = %v, want ErrNotFound", err)
	}
}

func TestTasksFilterByStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	status := "pending"
	tasks, err := s.Tasks(ctx, TaskFilter{Status: &status})
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Status != status {
			t.Errorf("task %d has status %q, want %q", task.ID, task.Status, status)
		}
		if task.Tags == nil {
			t.Errorf("task %d has nil Tags; expected non-nil slice", task.ID)
		}
	}
}

// TestCreateAndUpdateTask exercises the full write path so the assertions
// don't depend on exact seeded IDs: create a task, read it back, update its
// status, and confirm the change stuck.
func TestCreateAndUpdateTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	users, err := s.Users(ctx)
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) == 0 {
		t.Skip("no seeded users to attach a task to")
	}
	owner := users[0]

	due := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	created, err := s.CreateTask(ctx, NewTask{
		UserID:  owner.ID,
		Title:   "test task",
		DueDate: &due,
		Tags:    []string{"alpha", "beta", "beta"}, // duplicate should be de-duped
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Remove the created task when the test finishes (pass or fail) so runs
	// don't accumulate rows; task_tags go with it via ON DELETE CASCADE.
	t.Cleanup(func() {
		if _, err := s.db.ExecContext(context.Background(),
			`DELETE FROM tasks WHERE id = $1`, created.ID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	if created.Status != "pending" {
		t.Errorf("new task status = %q, want default %q", created.Status, "pending")
	}
	if len(created.Tags) != 2 {
		t.Errorf("new task tags = %v, want 2 unique", created.Tags)
	}

	got, err := s.TaskByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("TaskByID(%d): %v", created.ID, err)
	}
	if got.Title != "test task" || got.UserID != owner.ID {
		t.Errorf("TaskByID = %+v, want title/user to match created task", *got)
	}

	updated, err := s.UpdateTaskStatus(ctx, created.ID, "done")
	if err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	if updated.Status != "done" {
		t.Errorf("updated status = %q, want %q", updated.Status, "done")
	}

	if _, err := s.UpdateTaskStatus(ctx, 1<<62, "done"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateTaskStatus(missing) error = %v, want ErrNotFound", err)
	}
}