// Package gql defines the GraphQL schema and resolvers on top of the store.
package gql

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/graphql-go/graphql"

	"github.com/example/todo-homework/internal/store"
)

// Status values travel as PENDING/IN_PROGRESS/DONE in GraphQL but are
// stored as pending/in_progress/done in Postgres. These maps translate
// at the API boundary so the store only ever sees DB vocabulary.
var (
	statusToDB = map[string]string{
		"PENDING":     "pending",
		"IN_PROGRESS": "in_progress",
		"DONE":        "done",
	}
	statusToGQL = map[string]string{
		"pending":     "PENDING",
		"in_progress": "IN_PROGRESS",
		"done":        "DONE",
	}
)

const dateLayout = "2006-01-02"

// parseID converts a GraphQL ID (always a string on the wire) to an int64.
func parseID(v any) (int64, error) {
	s, ok := v.(string)
	if !ok {
		return 0, fmt.Errorf("invalid ID %v", v)
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid ID %q", s)
	}
	return id, nil
}

// NewSchema builds the executable schema, closing over the store. The logger
// is used by the resolver-timing extension (see tracing.go).
func NewSchema(s *store.Store, log *slog.Logger) (graphql.Schema, error) {
	taskStatusEnum := graphql.NewEnum(graphql.EnumConfig{
		Name: "TaskStatus",
		Values: graphql.EnumValueConfigMap{
			"PENDING":     &graphql.EnumValueConfig{Value: "PENDING"},
			"IN_PROGRESS": &graphql.EnumValueConfig{Value: "IN_PROGRESS"},
			"DONE":        &graphql.EnumValueConfig{Value: "DONE"},
		},
	})

	// User and Task reference each other (user.tasks / task.user), so we
	// declare both empty first and attach fields afterwards.
	userType := graphql.NewObject(graphql.ObjectConfig{Name: "User", Fields: graphql.Fields{}})
	taskType := graphql.NewObject(graphql.ObjectConfig{Name: "Task", Fields: graphql.Fields{}})

	taskType.AddFieldConfig("id", &graphql.Field{
		Type: graphql.NewNonNull(graphql.ID),
		Resolve: func(p graphql.ResolveParams) (any, error) {
			return strconv.FormatInt(p.Source.(store.Task).ID, 10), nil
		},
	})
	taskType.AddFieldConfig("title", &graphql.Field{
		Type: graphql.NewNonNull(graphql.String),
		Resolve: func(p graphql.ResolveParams) (any, error) {
			return p.Source.(store.Task).Title, nil
		},
	})
	taskType.AddFieldConfig("description", &graphql.Field{
		Type: graphql.String, // nullable, like the column
		Resolve: func(p graphql.ResolveParams) (any, error) {
			return p.Source.(store.Task).Description, nil
		},
	})
	taskType.AddFieldConfig("status", &graphql.Field{
		Type: graphql.NewNonNull(taskStatusEnum),
		Resolve: func(p graphql.ResolveParams) (any, error) {
			return statusToGQL[p.Source.(store.Task).Status], nil
		},
	})
	taskType.AddFieldConfig("dueDate", &graphql.Field{
		Type: graphql.String, // "YYYY-MM-DD" or null
		Resolve: func(p graphql.ResolveParams) (any, error) {
			d := p.Source.(store.Task).DueDate
			if d == nil {
				return nil, nil
			}
			return d.Format(dateLayout), nil
		},
	})
	taskType.AddFieldConfig("tags", &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(graphql.String))),
		Resolve: func(p graphql.ResolveParams) (any, error) {
			return p.Source.(store.Task).Tags, nil
		},
	})
	taskType.AddFieldConfig("user", &graphql.Field{
		Type: graphql.NewNonNull(userType),
		Resolve: func(p graphql.ResolveParams) (any, error) {
			u, err := s.UserByID(p.Context, p.Source.(store.Task).UserID)
			if err != nil {
				return nil, err
			}
			return *u, nil
		},
	})

	userType.AddFieldConfig("id", &graphql.Field{
		Type: graphql.NewNonNull(graphql.ID),
		Resolve: func(p graphql.ResolveParams) (any, error) {
			return strconv.FormatInt(p.Source.(store.User).ID, 10), nil
		},
	})
	userType.AddFieldConfig("email", &graphql.Field{
		Type: graphql.NewNonNull(graphql.String),
		Resolve: func(p graphql.ResolveParams) (any, error) {
			return p.Source.(store.User).Email, nil
		},
	})
	userType.AddFieldConfig("name", &graphql.Field{
		Type: graphql.NewNonNull(graphql.String),
		Resolve: func(p graphql.ResolveParams) (any, error) {
			return p.Source.(store.User).Name, nil
		},
	})
	userType.AddFieldConfig("tasks", &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(taskType))),
		Args: graphql.FieldConfigArgument{
			"status": &graphql.ArgumentConfig{Type: taskStatusEnum},
		},
		Resolve: func(p graphql.ResolveParams) (any, error) {
			u := p.Source.(store.User)
			f := store.TaskFilter{UserID: &u.ID}
			if v, ok := p.Args["status"].(string); ok {
				db := statusToDB[v]
				f.Status = &db
			}
			return s.Tasks(p.Context, f)
		},
	})

query := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"users": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(userType))),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					return s.Users(p.Context)
				},
			},
			"user": &graphql.Field{
				Type: userType, // nullable: unknown id resolves to null, not an error
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					id, err := parseID(p.Args["id"])
					if err != nil {
						return nil, err
					}
					u, err := s.UserByID(p.Context, id)
					if err == store.ErrNotFound {
						return nil, nil
					}
					if err != nil {
						return nil, err
					}
					return *u, nil
				},
			},
			"tasks": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(taskType))),
				Args: graphql.FieldConfigArgument{
					"status": &graphql.ArgumentConfig{Type: taskStatusEnum},
					"userId": &graphql.ArgumentConfig{Type: graphql.ID},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					var f store.TaskFilter
					if v, ok := p.Args["status"].(string); ok {
						db := statusToDB[v]
						f.Status = &db
					}
					if v, ok := p.Args["userId"]; ok && v != nil {
						id, err := parseID(v)
						if err != nil {
							return nil, err
						}
						f.UserID = &id
					}
					return s.Tasks(p.Context, f)
				},
			},
			"task": &graphql.Field{
				Type: taskType, // nullable, same reasoning as user
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					id, err := parseID(p.Args["id"])
					if err != nil {
						return nil, err
					}
					t, err := s.TaskByID(p.Context, id)
					if err == store.ErrNotFound {
						return nil, nil
					}
					if err != nil {
						return nil, err
					}
					return *t, nil
				},
			},
		},
	})
mutation := graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			"createTask": &graphql.Field{
				Type: graphql.NewNonNull(taskType),
				Args: graphql.FieldConfigArgument{
					"userId":      &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
					"title":       &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"description": &graphql.ArgumentConfig{Type: graphql.String},
					"dueDate":     &graphql.ArgumentConfig{Type: graphql.String},
					"tags":        &graphql.ArgumentConfig{Type: graphql.NewList(graphql.NewNonNull(graphql.String))},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					userID, err := parseID(p.Args["userId"])
					if err != nil {
						return nil, err
					}
					title, _ := p.Args["title"].(string)
					if title == "" {
						return nil, fmt.Errorf("title must not be empty")
					}

					in := store.NewTask{UserID: userID, Title: title}
					if v, ok := p.Args["description"].(string); ok && v != "" {
						in.Description = &v
					}
					if v, ok := p.Args["dueDate"].(string); ok && v != "" {
						d, err := time.Parse(dateLayout, v)
						if err != nil {
							return nil, fmt.Errorf("dueDate must be YYYY-MM-DD: %v", err)
						}
						in.DueDate = &d
					}
					if v, ok := p.Args["tags"].([]any); ok {
						for _, tag := range v {
							if str, ok := tag.(string); ok {
								in.Tags = append(in.Tags, str)
							}
						}
					}

					t, err := s.CreateTask(p.Context, in)
					if err != nil {
						return nil, err
					}
					return *t, nil
				},
			},
			"updateTaskStatus": &graphql.Field{
				Type: graphql.NewNonNull(taskType),
				Args: graphql.FieldConfigArgument{
					"id":     &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
					"status": &graphql.ArgumentConfig{Type: graphql.NewNonNull(taskStatusEnum)},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					id, err := parseID(p.Args["id"])
					if err != nil {
						return nil, err
					}
					// The TaskStatus enum already rejects anything outside
					// PENDING/IN_PROGRESS/DONE, so statusToDB is guaranteed a valid key.
					status, _ := p.Args["status"].(string)
					t, err := s.UpdateTaskStatus(p.Context, id, statusToDB[status])
					if err == store.ErrNotFound {
						return nil, fmt.Errorf("task %d not found", id)
					}
					if err != nil {
						return nil, err
					}
					return *t, nil
				},
			},
		},
	})
	

	return graphql.NewSchema(graphql.SchemaConfig{
		Query:      query,
		Mutation:   mutation,
		Extensions: []graphql.Extension{&tracer{log: log}},
	})
}