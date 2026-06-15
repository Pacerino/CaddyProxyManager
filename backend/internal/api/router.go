package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/Pacerino/CaddyProxyManager/embed"
	"github.com/Pacerino/CaddyProxyManager/internal/api/handler"
	"github.com/Pacerino/CaddyProxyManager/internal/api/middleware"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"

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

			// Per-host plugin configuration.
			r.Put("/{hostID:[0-9]+}/plugins", h.SetHostPlugin())                         // Create/update a host plugin
			r.Delete("/{hostID:[0-9]+}/plugins/{pluginID:[0-9]+}", h.DeleteHostPlugin()) // Remove a host plugin
		})

		//Caddy (modules, schemas & live config)
		r.With(middleware.Enforce()).Route("/caddy", func(r chi.Router) {
			r.Get("/modules", h.GetCaddyModules())           // List modules/plugins of the caddy build
			r.Get("/schemas", h.GetCaddySchemas())           // Global-scope config schemas
			r.Get("/host-schemas", h.GetHostScopedSchemas()) // Host-scope config schemas
			r.Get("/config", h.GetCaddyConfig())             // Read live caddy config (api mode)

			r.Route("/module-configs", func(r chi.Router) {
				r.Get("/", h.GetModuleConfigs())                 // List stored module configs
				r.Put("/", h.SetModuleConfig())                  // Create/update + apply a module config
				r.Delete("/{id:[0-9]+}", h.DeleteModuleConfig()) // Delete a module config
			})
		})

		//Auth
		r.Route("/auth", func(r chi.Router) {
			r.Get("/config", h.AuthConfig())          // Which auth mode is active
			r.Get("/oidc/login", h.OIDCLogin())       // Begin OIDC login
			r.Get("/oidc/callback", h.OIDCCallback()) // OIDC provider redirect
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

// frontendFS returns the filesystem used to serve the SPA. When
// CPM_FRONTENDDIR is set the assets are served from disk, otherwise the
// assets embedded in the binary are used.
func frontendFS() http.FileSystem {
	if dir := config.Configuration.FrontendDir; dir != "" {
		logger.Info("Serving frontend from external directory: %s", dir)
		return http.Dir(dir)
	}
	fSys, err := fs.Sub(embed.Assets, "assets")
	if err != nil {
		panic(err)
	}
	return http.FS(fSys)
}

func fileServer(r chi.Router) {
	root := frontendFS()
	fileSrv := http.FileServer(root)

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		// Serve the requested file if it exists, otherwise fall back to
		// index.html so client-side routing works (SPA behaviour).
		upath := strings.TrimPrefix(r.URL.Path, "/")
		if upath == "" {
			upath = "index.html"
		}
		if f, err := root.Open(upath); err != nil {
			r.URL.Path = "/"
		} else {
			f.Close()
		}
		fileSrv.ServeHTTP(w, r)
	})
}
