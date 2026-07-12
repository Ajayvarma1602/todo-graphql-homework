// Command server runs the Todo GraphQL API on :8080.
//
// Usage:
//
//	go run ./cmd/server
//
// Connection settings come from DATABASE_URL, falling back to the
// docker-compose defaults. The listen address can be overridden with ADDR.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/example/todo-homework/internal/gql"
	"github.com/example/todo-homework/internal/store"
)

const defaultDSN = "postgres://admin:todo@localhost:5432/homework?sslmode=disable"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	if err := run(log); err != nil {
		log.Error("fatal", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = defaultDSN
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(10)

	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return err
	}

	st := store.New(db)
	schema, err := gql.NewSchema(st)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/graphql", gql.Handler(schema, log))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			http.Error(w, "db unreachable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})
	// Convenience: serve the static UI so no separate web server is needed.
	if _, err := os.Stat("web/index.html"); err == nil {
		mux.Handle("/", http.FileServer(http.Dir("web")))
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", addr))
		errCh <- srv.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-stop:
		log.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}
	return nil
}