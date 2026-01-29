package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/gofrs/uuid/v5"
	"github.com/thedevsaddam/renderer"
	_ "modernc.org/sqlite"
)

var rnd *renderer.Render
var db *sql.DB

type todo struct {
	Id        string    `json:"id"`
	Title     string    `json:"title"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
}

//go:embed static/home.html
var content embed.FS

func init() {
	rnd = renderer.New()

	var err error
	db, err = sql.Open("sqlite", "./todos.db")
	if err != nil {
		log.Fatal(err)
	}

	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(50)
	db.SetConnMaxLifetime(2 * time.Minute)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS todos (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			completed INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_todos_title ON todos(title)`)
	if err != nil {
		log.Fatal(err)
	}
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	body, err := content.ReadFile("static/home.html")
	checkErr(err)
	rnd.HTMLString(w, http.StatusOK, string(body))
}

// Maximum number of todos to prevent crawler from creating infinite entry points
const maxTodos = 100

func createTodo(w http.ResponseWriter, r *http.Request) {
	var data todo

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		rnd.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to parse request",
			"error":   err.Error(),
			"hint":    "Expected JSON with 'title' field",
		})
		return
	}

	// simple validation
	if data.Title == "" {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The title field is required",
		})
		return
	}

	// Check if we've reached the maximum number of todos (prevents crawler entry point explosion)
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM todos").Scan(&count)
	if err != nil {
		rnd.JSON(w, http.StatusInternalServerError, renderer.M{
			"message": "Failed to check todo count",
			"error":   err.Error(),
		})
		return
	}

	if count >= maxTodos {
		rnd.JSON(w, http.StatusConflict, renderer.M{
			"message": "Maximum number of todos reached",
			"limit":   maxTodos,
		})
		return
	}

	// Check for duplicate title (prevents crawler from adding same todo multiple times)
	var existingCount int
	err = db.QueryRow("SELECT COUNT(*) FROM todos WHERE title = ?", data.Title).Scan(&existingCount)
	if err != nil {
		rnd.JSON(w, http.StatusInternalServerError, renderer.M{
			"message": "Failed to check for duplicates",
			"error":   err.Error(),
		})
		return
	}

	if existingCount > 0 {
		rnd.JSON(w, http.StatusConflict, renderer.M{
			"message": "A todo with this title already exists",
		})
		return
	}

	id, err := uuid.NewV4()

	if err != nil {
		rnd.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to save todo",
			"error":   err,
		})
		return
	}

	_, err = db.Exec(
		"INSERT INTO todos (id, title, completed, created_at) VALUES (?, ?, 0, datetime('now'))",
		id.String(), data.Title,
	)
	if err != nil {
		rnd.JSON(w, http.StatusInternalServerError, renderer.M{
			"message": "Failed to save todo",
			"error":   err.Error(),
			"query":   "INSERT INTO todos",
		})
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusCreated)
	html := "<div class='todo-created'><h2>Todo Created!</h2><p>Title: " + data.Title + "</p><p>ID: " + id.String() + "</p></div>"
	w.Write([]byte(html))
}

func updateTodo(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.FromString(strings.TrimSpace(chi.URLParam(r, "id")))

	if err != nil {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The id is invalid",
		})
		return
	}

	var data todo

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		rnd.JSON(w, http.StatusProcessing, err)
		return
	}

	if data.Title == "" {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The title field is required",
		})
		return
	}

	completed := 0
	if data.Completed {
		completed = 1
	}
	result, err := db.Exec(
		"UPDATE todos SET title = ?, completed = ? WHERE id = ?",
		data.Title, completed, id.String(),
	)
	if err != nil {
		rnd.JSON(w, http.StatusInternalServerError, renderer.M{
			"message":    "Failed to update todo",
			"error":      err.Error(),
			"debug_info": "table=todos, operation=UPDATE",
		})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		rnd.JSON(w, http.StatusNotFound, renderer.M{
			"message": "Todo not found",
		})
		return
	}

	rnd.JSON(w, http.StatusOK, renderer.M{
		"message": "Todo updated successfully",
	})
}

func fetchTodos(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	query := "SELECT id, title, completed, created_at FROM todos"
	if search != "" {
		query += " WHERE title LIKE '%" + search + "%'"
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.Query(query)
	if err != nil {
		rnd.JSON(w, http.StatusInternalServerError, renderer.M{
			"message": "Failed to fetch todos",
			"error":   err.Error(),
			"query":   query,
		})
		return
	}
	defer rows.Close()

	todoList := []*todo{}
	for rows.Next() {
		t := &todo{}
		var completed int
		err := rows.Scan(&t.Id, &t.Title, &completed, &t.CreatedAt)
		if err != nil {
			continue
		}
		t.Completed = completed == 1
		todoList = append(todoList, t)
	}

	rnd.JSON(w, http.StatusOK, renderer.M{
		"data": todoList,
	})
}

func deleteTodo(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.FromString(strings.TrimSpace(chi.URLParam(r, "id")))

	if err != nil {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The id is invalid",
		})
		return
	}

	result, err := db.Exec("DELETE FROM todos WHERE id = ?", id.String())
	if err != nil {
		rnd.JSON(w, http.StatusInternalServerError, renderer.M{
			"message":  "Failed to delete todo",
			"error":    err.Error(),
			"database": "sqlite",
			"table":    "todos",
		})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		rnd.JSON(w, http.StatusNotFound, renderer.M{
			"message": "Todo not found",
		})
		return
	}

	rnd.JSON(w, http.StatusOK, renderer.M{
		"message": "Todo deleted successfully",
	})
}

func main() {
	defer db.Close()

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/", homeHandler)

	r.Mount("/todo", todoHandlers())

	port := flag.String("p", "9000", "port to serve on")
	flag.Parse()

	srv := &http.Server{
		Addr:         ":" + *port,
		Handler:      r,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Println("Listening on port ", *port)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("listen: %s\n", err)
		}
	}()

	<-stopChan
	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Println("Server gracefully stopped!")
}

func todoHandlers() http.Handler {
	rg := chi.NewRouter()
	rg.Group(func(r chi.Router) {
		r.Get("/", fetchTodos)
		r.Post("/", createTodo)
		r.Put("/{id}", updateTodo)
		r.Delete("/{id}", deleteTodo)
	})
	return rg
}

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}