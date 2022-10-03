package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/Pacerino/CaddyProxyManager/embed"
	"github.com/Pacerino/CaddyProxyManager/internal/api/handler"
	"github.com/Pacerino/CaddyProxyManager/internal/api/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func NewRouter() http.Handler {
	cors := cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           300,
	})

	r := chi.NewRouter()
	h := handler.NewHandler()

	r.Use(
		cors,
		middleware.DecodeAuth(),
	)

	return generateRoutes(r, h)
}

func generateRoutes(r chi.Router, h *handler.Handler) chi.Router {
	r.Route("/api", func(r chi.Router) {

		//Hosts
		r.With(middleware.Enforce()).Route("/hosts", func(r chi.Router) {
			r.Get("/", h.GetHosts())                     // Get List of Hosts
			r.Post("/", h.CreateHost())                  // Create Host & save to the DB
			r.Get("/{hostID:[0-9]+}", h.GetHost())       // Get specific Host by ID
			r.Delete("/{hostID:[0-9]+}", h.DeleteHost()) // Delete Host by ID
			r.Put("/", h.UpdateHost())                   // Update Host by ID
		})

		//User
		r.Route("/users", func(r chi.Router) {
			r.Post("/login", h.UserLogin())                                         // Login a User
			r.With(middleware.Enforce()).Get("/", h.GetUsers())                     // Get a list of Users
			r.With(middleware.Enforce()).Post("/", h.CreateUser())                  // Create a User
			r.With(middleware.Enforce()).Get("/{userID:[0-9]+}", h.GetUser())       // Get a User by ID
			r.With(middleware.Enforce()).Delete("/{userID:[0-9]+}", h.DeleteUser()) // Delete User by ID
			r.With(middleware.Enforce()).Put("/", h.UpdateUser())                   // Update Host by ID
		})
	})
	fileServer(r)
	return r
}

func fileServer(r chi.Router) {
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		pathPrefix := strings.TrimSuffix(rctx.RoutePattern(), "/*")
		fSys, err := fs.Sub(embed.Assets, "assets")
		if err != nil {
			panic(err)
		}
		fs := http.StripPrefix(pathPrefix, http.FileServer(http.FS(fSys)))
		fs.ServeHTTP(w, r)
	})
}
