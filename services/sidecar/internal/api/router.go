package api

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

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
	ArtifactDir    string
}

type API struct {
	db            *gorm.DB
	options       Options
	artifactStore *artifactStore
}

type Router struct {
	*gin.Engine
	artifactStore *artifactStore
	closeOnce     sync.Once
	closeErr      error
}

func (r *Router) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		if r.artifactStore != nil {
			r.closeErr = r.artifactStore.close()
		}
	})
	return r.closeErr
}

func NewRouter(db *gorm.DB, options Options) (*Router, error) {
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

	var artifacts *artifactStore
	if options.ArtifactDir != "" {
		var databaseID string
		var boundStoreID sql.NullString
		if err := db.Raw("SELECT database_id, artifact_store_id FROM workspace_identity WHERE singleton = 1").Row().Scan(&databaseID, &boundStoreID); err != nil {
			return nil, err
		}
		var err error
		artifacts, err = newArtifactStore(options.ArtifactDir, databaseID, boundStoreID.String)
		if err != nil {
			return nil, err
		}
		if !boundStoreID.Valid {
			result := db.Exec(
				"UPDATE workspace_identity SET artifact_store_id = ? WHERE singleton = 1 AND artifact_store_id IS NULL",
				artifacts.storeID,
			)
			if result.Error != nil {
				_ = artifacts.close()
				return nil, fmt.Errorf("bind Artifact root to workspace database: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				var current string
				if err := db.Raw("SELECT artifact_store_id FROM workspace_identity WHERE singleton = 1").Row().Scan(&current); err != nil || current != artifacts.storeID {
					_ = artifacts.close()
					return nil, errors.New("workspace database was concurrently bound to a different Artifact root")
				}
			}
		}
		if err := artifacts.reconcile(db); err != nil {
			_ = artifacts.close()
			return nil, err
		}
	}
	service := &API{db: db, options: options, artifactStore: artifacts}
	router.GET("/health", service.health)
	v1 := router.Group("/api/" + Version)
	{
		v1.GET("/actors", service.listActors)
		v1.POST("/actors", service.createActor)
		v1.GET("/actors/:id", service.getActor)
		v1.PATCH("/actors/:id", service.updateActor)
		v1.GET("/tasks", service.listTasks)
		v1.POST("/tasks", service.createTask)
		v1.PATCH("/tasks/batch", service.batchUpdateTasks)
		v1.PUT("/tasks/reorder", service.reorderTasks)
		v1.GET("/tasks/:id", service.getTask)
		v1.PATCH("/tasks/:id", service.updateTask)
		v1.PATCH("/tasks/:id/status", service.updateTaskStatus)
		v1.POST("/tasks/:id/start", service.startTask)
		v1.POST("/tasks/:id/block", service.blockTask)
		v1.POST("/tasks/:id/unblock", service.unblockTask)
		v1.POST("/tasks/:id/complete", service.completeTask)
		v1.POST("/tasks/:id/cancel", service.cancelTask)
		v1.POST("/tasks/:id/reopen", service.reopenTask)
		v1.POST("/tasks/:id/submit-output", service.submitTaskOutput)
		v1.POST("/tasks/:id/review", service.reviewTaskOutput)
		v1.GET("/tasks/:id/submissions", service.listTaskSubmissions)
		v1.GET("/tasks/:id/artifacts", service.listTaskArtifacts)
		v1.GET("/tasks/:id/events", service.listTaskWorkflowEvents)
		v1.GET("/tasks/:id/assignments", service.listTaskAssignments)
		v1.POST("/tasks/:id/assignments", service.createTaskAssignment)
		v1.POST("/tasks/:id/reassign", service.reassignTask)
		v1.POST("/assignments/:id/end", service.endAssignment)
		v1.GET("/artifacts/:id", service.getTaskArtifact)
		v1.GET("/artifacts/:id/content", service.getTaskArtifactContent)
		v1.DELETE("/artifacts/:id", service.deleteTaskArtifact)
		v1.DELETE("/tasks/:id", service.deleteTask)
		v1.GET("/tags", service.listTags)
		v1.POST("/tags", service.createTag)
		v1.PATCH("/tags/:id", service.updateTag)
		v1.DELETE("/tags/:id", service.deleteTag)
		v1.GET("/projects", service.listProjects)
		v1.POST("/projects", service.createProject)
		v1.GET("/projects/:id", service.getProject)
		v1.PATCH("/projects/:id", service.updateProject)
		v1.POST("/projects/:id/transitions", service.transitionProject)
		v1.DELETE("/projects/:id", service.deleteProject)
		v1.GET("/clients", service.listClients)
		v1.POST("/clients", service.createClient)
		v1.GET("/clients/:id", service.getClient)
		v1.PATCH("/clients/:id", service.updateClient)
		v1.DELETE("/clients/:id", service.deleteClient)
		v1.GET("/stats/today", service.todayStats)
	}
	return &Router{Engine: router, artifactStore: artifacts}, nil
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
