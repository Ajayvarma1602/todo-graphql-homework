package store

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestQuick(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://admin:todo@localhost:5432/homework?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	s := New(db)
	users, err := s.Users(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("got %d users, first: %+v", len(users), users[0])


	
	status := "pending"
	tasks, err := s.Tasks(context.Background(), TaskFilter{Status: &status})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("pending tasks: %d, first tags: %v", len(tasks), tasks[0].Tags)

}


