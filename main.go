package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/gofrs/uuid/v5"
	"github.com/thedevsaddam/renderer"
)

var rnd *renderer.Render

type todo struct {
	Id        string    `json:"id"`
	Title     string    `json:"title"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
}

//go:embed static/home.html
var content embed.FS
var todos = make(map[string]*todo)
var todosMutex = sync.RWMutex{}

func init() {
	rnd = renderer.New()
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	body, err := content.ReadFile("static/home.html")
	checkErr(err)
	rnd.HTMLString(w, http.StatusOK, string(body))
}

func createTodo(w http.ResponseWriter, r *http.Request) {
	var data todo

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		rnd.JSON(w, http.StatusProcessing, err)
		return
	}

	// simple validation
	if data.Title == "" {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The title field is required",
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
	t := todo{
		Id:        id.String(),
		Title:     data.Title,
		Completed: false,
		CreatedAt: time.Now(),
	}

	todosMutex.Lock()
	todos[t.Id] = &t
	todosMutex.Unlock()

	rnd.JSON(w, http.StatusCreated, renderer.M{
		"message": "Todo created successfully",
		"todo_id": t.Id,
	})
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

	// simple validation
	if data.Title == "" {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The title field is required",
		})
		return
	}

	todosMutex.Lock()
	t := todos[id.String()]
	todosMutex.Unlock()

	if t == nil {
		rnd.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to update todo",
		})
		return
	}

	t.Title = data.Title
	t.Completed = data.Completed

	rnd.JSON(w, http.StatusOK, renderer.M{
		"message": "Todo updated successfully",
	})
}

func fetchTodos(w http.ResponseWriter, r *http.Request) {
	todoList := []*todo{}

	todosMutex.Lock()
	for _, t := range todos {
		todoList = append(todoList, t)
	}
	todosMutex.Unlock()

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

	todosMutex.Lock()
	delete(todos, id.String())
	todosMutex.Unlock()

	rnd.JSON(w, http.StatusOK, renderer.M{
		"message": "Todo deleted successfully",
	})
}

func main() {
	stopChan := make(chan os.Signal)
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
	srv.Shutdown(ctx)
	defer cancel()
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
