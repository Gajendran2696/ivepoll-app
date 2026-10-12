package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"livepoll/config"
	"livepoll/handlers"
	"livepoll/middleware"
	"livepoll/realtime"
	"livepoll/store"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.New(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Fatalf("mongodb: %v", err)
	}
	broker, err := realtime.NewBroker(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	hub := realtime.NewHub(broker)
	go hub.Run(ctx)

	app := &handlers.App{Cfg: cfg, Store: db, Broker: broker, Hub: hub}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newRouter(app, cfg),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: it would kill long-lived WebSocket connections.
	}

	go func() {
		log.Printf("LivePoll listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	_ = db.Close(shutdownCtx)
	_ = broker.Close()
}

func newRouter(app *handlers.App, cfg *config.Config) *gin.Engine {
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger(), middleware.SecurityHeaders())
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Fatalf("TRUSTED_PROXIES: %v", err)
	}

	if len(cfg.AllowedOrigins) > 0 {
		r.Use(cors.New(cors.Config{
			AllowOrigins: cfg.AllowedOrigins,
			AllowMethods: []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
			AllowHeaders: []string{"Authorization", "Content-Type", "X-Voter-Id"},
			MaxAge:       12 * time.Hour,
		}))
	}

	r.GET("/healthz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := app.Store.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "mongodb down"})
			return
		}
		if err := app.Broker.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "redis down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api", middleware.BodyLimit(64<<10))
	{
		// Brute-force protection on credential endpoints.
		authLimit := middleware.RateLimit(app.Broker, "auth", 10, time.Minute)
		api.POST("/auth/signup", authLimit, app.Signup)
		api.POST("/auth/login", authLimit, app.Login)
		api.GET("/auth/me", middleware.Auth(cfg.JWTSecret), app.Me)

		// Owner-only: creating and managing polls requires a login.
		secured := api.Group("", middleware.Auth(cfg.JWTSecret))
		secured.POST("/polls", app.CreatePoll)
		secured.GET("/polls", app.ListMyPolls)
		secured.PATCH("/polls/:slug", app.UpdatePoll)
		secured.DELETE("/polls/:slug", app.DeletePoll)

		// Public: anyone with the link can view and vote.
		api.GET("/polls/:slug", middleware.OptionalAuth(cfg.JWTSecret), app.GetPoll)
		api.POST("/polls/:slug/vote", middleware.RateLimit(app.Broker, "vote", 30, time.Minute), app.Vote)
	}

	r.GET("/ws/polls/:slug", middleware.RateLimit(app.Broker, "ws", 60, time.Minute), app.WatchPoll)

	serveFrontend(r, cfg.StaticDir)
	return r
}

// serveFrontend lets a single deployed service host both the API and the built React app.
// The frontend and backend code still live in separate folders; this only serves build output.
func serveFrontend(r *gin.Engine, dir string) {
	if dir == "" {
		return
	}
	index := filepath.Join(dir, "index.html")
	if _, err := os.Stat(index); err != nil {
		log.Printf("STATIC_DIR %q has no index.html, not serving frontend", dir)
		return
	}
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/ws/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		// Serve real files (assets, favicon); everything else is a client-side route.
		clean := filepath.Join(dir, filepath.Clean("/"+p))
		if st, err := os.Stat(clean); err == nil && !st.IsDir() {
			if strings.HasPrefix(p, "/assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			c.File(clean)
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.File(index)
	})
}
