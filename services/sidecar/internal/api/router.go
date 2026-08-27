package api

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const Version = "v1"

type Options struct {
	AppVersion     string
	Commit         string
	SchemaVersion  int
	SessionToken   string
	DevMode        bool
	AllowedOrigins []string
	Logger         *log.Logger
}

type API struct {
	db      *gorm.DB
	options Options
}

func NewRouter(db *gorm.DB, options Options) (*gin.Engine, error) {
	if err := middlewareConfigurationError(options.SessionToken, options.DevMode, options.AllowedOrigins); err != nil {
		return nil, err
	}
	if options.Logger == nil {
		options.Logger = log.New(os.Stderr, "sidecar ", log.Ldate|log.Ltime|log.LUTC)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true
	if err := router.SetTrustedProxies(nil); err != nil {
		return nil, err
	}
	router.Use(
		requestIDMiddleware(),
		originMiddleware(options.AllowedOrigins),
		authMiddleware(options.SessionToken, options.DevMode),
		accessLogMiddleware(options.Logger),
		recoveryMiddleware(options.Logger),
	)
	router.NoRoute(func(c *gin.Context) {
		writeError(c, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route not found")
	})
	router.NoMethod(func(c *gin.Context) {
		writeError(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	})

	service := &API{db: db, options: options}
	router.GET("/health", service.health)
	v1 := router.Group("/api/" + Version)
	{
		v1.GET("/tasks", service.listTasks)
		v1.POST("/tasks", service.createTask)
		v1.GET("/tasks/:id", service.getTask)
		v1.PATCH("/tasks/:id", service.updateTaskStatus)
		v1.PATCH("/tasks/:id/status", service.updateTaskStatus)
		v1.DELETE("/tasks/:id", service.deleteTask)
		v1.GET("/stats/today", service.todayStats)
	}
	return router, nil
}

func (a *API) health(c *gin.Context) {
	sqlDB, err := a.db.DB()
	if err != nil || sqlDB.PingContext(c.Request.Context()) != nil {
		writeError(c, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "The local database is unavailable")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"app": gin.H{
			"name":    "opc-workspace",
			"version": a.options.AppVersion,
			"commit":  a.options.Commit,
		},
		"api":    gin.H{"version": Version},
		"schema": gin.H{"version": a.options.SchemaVersion},
	})
}
