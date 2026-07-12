# syntax=docker/dockerfile:1

# --- build stage: compile both binaries from the module ---
FROM golang:1.25-alpine AS build
WORKDIR /src

# Download deps first so this layer is cached unless go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static binaries (pgx is pure Go, so CGO can stay off).
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -o /out/seed   ./cmd/seed

# --- runtime stage: minimal image with just the binaries and the UI ---
FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=build /out/seed   /app/seed
# The server serves web/index.html relative to its working dir.
COPY web /app/web

EXPOSE 8080
CMD ["/app/server"]
