package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const Version = "v1"

type Options struct {
	AppVersion             string
	Commit                 string
	SchemaVersion          int
	SessionToken           string
	DevMode                bool
	AllowedOrigins         []string
	Logger                 *log.Logger
	ArtifactDir            string
	DatabasePath           string
	BackupDir              string
	LogDir                 string
	DiskSpaceCheck         func(path string) (availableBytes uint64, totalBytes uint64, err error)
	VolumeIdentityCheck    func(path string) (identity string, err error)
	DiskSpaceScanInterval  time.Duration
	Now                    func() time.Time
	FocusHeartbeatInterval time.Duration
	ReminderScanInterval   time.Duration
	StartupRestore         StartupRestoreResult
}

type API struct {
	db                    *gorm.DB
	options               Options
	artifactStore         *artifactStore
	backupStore           *backupStore
	maintenance           *sync.RWMutex
	restorePending        atomic.Bool
	lowDiskActive         atomic.Bool
	lowDiskThresholdBytes atomic.Uint64
}

type Router struct {
	*gin.Engine
	artifactStore        *artifactStore
	focusHeartbeatCancel context.CancelFunc
	focusHeartbeatDone   chan struct{}
	reminderScanCancel   context.CancelFunc
	reminderScanDone     chan struct{}
	diskSpaceScanCancel  context.CancelFunc
	diskSpaceScanDone    chan struct{}
	closeOnce            sync.Once
	closeErr             error
}

func (r *Router) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		if r.focusHeartbeatCancel != nil {
			r.focusHeartbeatCancel()
			<-r.focusHeartbeatDone
		}
		if r.reminderScanCancel != nil {
			r.reminderScanCancel()
			<-r.reminderScanDone
		}
		if r.diskSpaceScanCancel != nil {
			r.diskSpaceScanCancel()
			<-r.diskSpaceScanDone
		}
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
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.FocusHeartbeatInterval == 0 {
		options.FocusHeartbeatInterval = 15 * time.Second
	}
	if options.ReminderScanInterval == 0 {
		options.ReminderScanInterval = 15 * time.Second
	}
	if options.DiskSpaceScanInterval == 0 {
		if strings.TrimSpace(options.LogDir) == "" {
			options.DiskSpaceScanInterval = -1
		} else {
			options.DiskSpaceScanInterval = 5 * time.Minute
		}
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
		var err error
		artifacts, err = openWorkspaceArtifactStore(db, options.ArtifactDir, true)
		if err != nil {
			return nil, err
		}
	}
	if err := recoverFocusSessionsOnStartup(db, options.Now().UTC().Truncate(time.Second)); err != nil {
		if artifacts != nil {
			_ = artifacts.close()
		}
		return nil, fmt.Errorf("recover active Focus Sessions: %w", err)
	}
	var backups *backupStore
	if options.BackupDir != "" || options.DatabasePath != "" {
		var err error
		backups, err = newBackupStore(options.BackupDir, options.DatabasePath, artifacts)
		if err != nil {
			if artifacts != nil {
				_ = artifacts.close()
			}
			return nil, err
		}
	}
	service := &API{
		db: db, options: options, artifactStore: artifacts, backupStore: backups,
		maintenance: &sync.RWMutex{},
	}
	if err := service.ensureAutomationRules(options.Now().UTC()); err != nil {
		if artifacts != nil {
			_ = artifacts.close()
		}
		return nil, fmt.Errorf("initialize preset Automations: %w", err)
	}
	if err := service.projectDueAutomations(options.Now().UTC()); err != nil {
		if artifacts != nil {
			_ = artifacts.close()
		}
		return nil, fmt.Errorf("project due Automations: %w", err)
	}
	if err := service.projectDueReminders(context.Background()); err != nil {
		if artifacts != nil {
			_ = artifacts.close()
		}
		return nil, fmt.Errorf("project due Reminders: %w", err)
	}
	if err := service.projectDueClientFollowups(context.Background()); err != nil {
		if artifacts != nil {
			_ = artifacts.close()
		}
		return nil, fmt.Errorf("project due Client Followups: %w", err)
	}
	if err := service.projectDueTasks(context.Background()); err != nil {
		if artifacts != nil {
			_ = artifacts.close()
		}
		return nil, fmt.Errorf("project due Tasks: %w", err)
	}
	if err := service.projectDueContentItems(context.Background()); err != nil {
		if artifacts != nil {
			_ = artifacts.close()
		}
		return nil, fmt.Errorf("project due Content Items: %w", err)
	}
	if options.DiskSpaceScanInterval > 0 {
		if err := service.scanDiskSpace(); err != nil && options.Logger != nil {
			options.Logger.Print("storage capacity check could not be completed safely")
		}
	}
	router.GET("/health", service.health)
	v1 := router.Group("/api/" + Version)
	v1.Use(service.maintenanceReadMiddleware())
	{
		v1.GET("/diagnostics/package", service.downloadDiagnosticPackage)
		v1.GET("/diagnostics/storage", service.getStorageCapacity)
		v1.GET("/exports/business-data", service.exportBusinessData)
		v1.GET("/exports/business-package", service.exportBusinessPackage)
		v1.POST("/imports/business-data/preview", service.previewBusinessImport)
		v1.POST("/imports/business-data", service.applyBusinessImport)
		v1.POST("/imports/business-package/preview", service.previewBusinessPackageImport)
		v1.POST("/imports/business-package", service.applyBusinessPackageImport)
		v1.GET("/backups", service.listBackups)
		v1.GET("/backups/restore-diagnostics", service.getRestoreDiagnostics)
		v1.POST("/backups", service.createBackup)
		v1.POST("/backups/:id/verify", service.verifyBackup)
		v1.POST("/backups/:id/drill", service.drillBackupRestore)
		v1.POST("/backups/:id/restore", service.scheduleBackupRestore)
		v1.DELETE("/backups/:id", service.deleteBackup)
		v1.GET("/settings", service.listSettings)
		v1.PATCH("/settings", service.updateSettings)
		v1.POST("/settings/avatar", service.commitSettingsWithAvatar)
		v1.GET("/settings/avatar/content", service.getWorkspaceAvatarContent)
		v1.GET("/actors", service.listActors)
		v1.POST("/actors", service.createActor)
		v1.GET("/actors/:id", service.getActor)
		v1.PATCH("/actors/:id", service.updateActor)
		v1.GET("/agent-adapters", service.listAgentAdapters)
		v1.POST("/agent-adapters", service.createAgentAdapter)
		v1.GET("/agent-adapters/:id", service.getAgentAdapter)
		v1.POST("/agent-adapters/:id/check", service.checkAgentAdapter)
		v1.POST("/agent-adapters/:id/enable", service.enableAgentAdapter)
		v1.POST("/agent-adapters/:id/disable", service.disableAgentAdapter)
		v1.GET("/search", service.search)
		v1.GET("/tasks", service.listTasks)
		v1.POST("/tasks", service.createTask)
		v1.GET("/task-saved-views", service.listTaskSavedViews)
		v1.POST("/task-saved-views", service.createTaskSavedView)
		v1.PATCH("/task-saved-views/:id", service.updateTaskSavedView)
		v1.DELETE("/task-saved-views/:id", service.deleteTaskSavedView)
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
		v1.GET("/projects/:id/events", service.listProjectWorkflowEvents)
		v1.GET("/projects/:id/artifacts", service.listProjectArtifacts)
		v1.GET("/projects/:id/attachments", service.listProjectAttachments)
		v1.POST("/projects/:id/attachments", service.createProjectAttachment)
		v1.GET("/projects/:id/notes", service.listProjectNotes)
		v1.POST("/projects/:id/notes", service.createProjectNote)
		v1.PATCH("/projects/:id", service.updateProject)
		v1.POST("/projects/:id/transitions", service.transitionProject)
		v1.DELETE("/projects/:id", service.deleteProject)
		v1.GET("/project-notes/:id", service.getProjectNote)
		v1.PATCH("/project-notes/:id", service.updateProjectNote)
		v1.DELETE("/project-notes/:id", service.deleteProjectNote)
		v1.GET("/project-attachments/:id", service.getProjectAttachment)
		v1.GET("/project-attachments/:id/content", service.getProjectAttachmentContent)
		v1.DELETE("/project-attachments/:id", service.deleteProjectAttachment)
		v1.GET("/roadmap/milestones", service.listRoadmapMilestones)
		v1.POST("/roadmap/milestones", service.createRoadmapMilestone)
		v1.PUT("/roadmap/milestones/reorder", service.reorderRoadmapMilestones)
		v1.GET("/roadmap/milestones/:id", service.getRoadmapMilestone)
		v1.PATCH("/roadmap/milestones/:id", service.updateRoadmapMilestone)
		v1.POST("/roadmap/milestones/:id/archive", service.archiveRoadmapMilestone)
		v1.POST("/roadmap/milestones/:id/restore", service.restoreRoadmapMilestone)
		v1.DELETE("/roadmap/milestones/:id", service.deleteRoadmapMilestone)
		v1.GET("/content-items", service.listContentItems)
		v1.POST("/content-items", service.createContentItem)
		v1.GET("/content-items/:id", service.getContentItem)
		v1.PATCH("/content-items/:id", service.updateContentItem)
		v1.DELETE("/content-items/:id", service.deleteContentItem)
		v1.PUT("/content-items/:id/schedule", service.scheduleContentItem)
		v1.POST("/content-items/:id/publish-confirmation", service.confirmContentItemPublished)
		v1.GET("/content-items/:id/tasks", service.listContentItemTasks)
		v1.POST("/content-items/:id/tasks", service.linkContentItemTask)
		v1.DELETE("/content-items/:id/tasks/:taskId", service.unlinkContentItemTask)
		v1.GET("/clients", service.listClients)
		v1.POST("/clients", service.createClient)
		v1.GET("/clients/:id", service.getClient)
		v1.PATCH("/clients/:id", service.updateClient)
		v1.DELETE("/clients/:id", service.deleteClient)
		v1.GET("/clients/:id/activities", service.listClientActivities)
		v1.POST("/clients/:id/activities", service.createClientActivity)
		v1.GET("/client-activities/:id", service.getClientActivity)
		v1.PATCH("/client-activities/:id", service.updateClientActivity)
		v1.DELETE("/client-activities/:id", service.deleteClientActivity)
		v1.GET("/clients/:id/attachments", service.listClientAttachments)
		v1.POST("/clients/:id/attachments", service.createClientAttachment)
		v1.GET("/client-attachments/:id", service.getClientAttachment)
		v1.GET("/client-attachments/:id/content", service.getClientAttachmentContent)
		v1.DELETE("/client-attachments/:id", service.deleteClientAttachment)
		v1.GET("/clients/:id/actor-links", service.listClientActorLinks)
		v1.POST("/clients/:id/actor-links", service.createClientActorLink)
		v1.DELETE("/client-actor-links/:id", service.deleteClientActorLink)
		v1.GET("/client-followups", service.listClientFollowups)
		v1.POST("/client-followups", service.createClientFollowup)
		v1.GET("/client-followups/:id", service.getClientFollowup)
		v1.PATCH("/client-followups/:id", service.updateClientFollowup)
		v1.DELETE("/client-followups/:id", service.cancelClientFollowup)
		v1.POST("/client-followups/:id/complete", service.completeClientFollowup)
		v1.POST("/client-followups/:id/skip", service.skipClientFollowup)
		v1.POST("/client-followups/:id/reschedule", service.rescheduleClientFollowup)
		v1.GET("/clients/:id/followups", service.listClientFollowupsForClientRoute)
		v1.GET("/focus-sessions", service.listFocusSessions)
		v1.GET("/focus-sessions/active", service.getActiveFocusSession)
		v1.POST("/focus-sessions", service.createFocusSession)
		v1.POST("/focus-sessions/:id/pause", service.pauseFocusSession)
		v1.POST("/focus-sessions/:id/resume", service.resumeFocusSession)
		v1.POST("/focus-sessions/:id/recover", service.recoverFocusSession)
		v1.POST("/focus-sessions/:id/stop", service.stopFocusSession)
		v1.POST("/focus-sessions/:id/cancel", service.cancelFocusSession)
		v1.GET("/inbox-items", service.listInboxItems)
		v1.POST("/inbox-items", service.createInboxItem)
		v1.POST("/inbox-items/read-all", service.readAllInboxItems)
		v1.GET("/inbox-items/:id", service.getInboxItem)
		v1.PATCH("/inbox-items/:id", service.updateInboxItem)
		v1.GET("/inbox-items/:id/events", service.listInboxItemEvents)
		v1.GET("/inbox-items/:id/tasks", service.listInboxItemTasks)
		v1.POST("/inbox-items/:id/split", service.splitInboxItem)
		v1.POST("/inbox-items/:id/tasks/:task_id", service.linkInboxItemTask)
		v1.PATCH("/inbox-items/:id/tasks/:task_id", service.updateInboxItemTask)
		v1.DELETE("/inbox-items/:id/tasks/:task_id", service.unlinkInboxItemTask)
		v1.POST("/inbox-items/:id/read", service.readInboxItem)
		v1.POST("/inbox-items/:id/snooze", service.snoozeInboxItem)
		v1.POST("/inbox-items/:id/unsnooze", service.unsnoozeInboxItem)
		v1.POST("/inbox-items/:id/resolve", service.resolveInboxItem)
		v1.POST("/inbox-items/:id/force-resolve", service.forceResolveInboxItem)
		v1.POST("/inbox-items/:id/dismiss", service.dismissInboxItem)
		v1.POST("/inbox-items/:id/reopen", service.reopenInboxItem)
		v1.GET("/reminders", service.listReminders)
		v1.POST("/reminders", service.createReminder)
		v1.GET("/reminders/:id", service.getReminder)
		v1.PATCH("/reminders/:id", service.updateReminder)
		v1.DELETE("/reminders/:id", service.cancelReminder)
		v1.GET("/automations/rules", service.listAutomationRules)
		v1.GET("/automations/rules/:id", service.getAutomationRule)
		v1.PATCH("/automations/rules/:id", service.updateAutomationRule)
		v1.POST("/automations/rules/:id/preview", service.previewAutomationRule)
		v1.POST("/automations/rules/:id/enable", service.enableAutomationRule)
		v1.POST("/automations/rules/:id/disable", service.disableAutomationRule)
		v1.GET("/automations/runs", service.listAutomationRuns)
		v1.GET("/automations/runs/:id", service.getAutomationRun)
		v1.POST("/automations/runs/:id/retry", service.retryAutomationRun)
		v1.GET("/stats/today", service.todayStats)
		v1.GET("/stats/focus", service.focusPeriodStats)
		v1.GET("/stats/inbox", service.inboxStats)
	}
	result := &Router{Engine: router, artifactStore: artifacts}
	if options.FocusHeartbeatInterval > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		result.focusHeartbeatCancel = cancel
		result.focusHeartbeatDone = make(chan struct{})
		go func() {
			defer close(result.focusHeartbeatDone)
			ticker := time.NewTicker(options.FocusHeartbeatInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if service.restorePending.Load() {
						continue
					}
					service.maintenance.RLock()
					err := service.refreshActiveFocusHeartbeat(ctx)
					service.maintenance.RUnlock()
					if err != nil && options.Logger != nil && !errors.Is(err, context.Canceled) {
						options.Logger.Printf("Focus Session heartbeat failed: %v", err)
					}
					if err != nil && !errors.Is(err, context.Canceled) {
						service.recordRuntimeDatabaseFailure("focus-heartbeat")
					}
				}
			}
		}()
	}
	if options.ReminderScanInterval > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		result.reminderScanCancel = cancel
		result.reminderScanDone = make(chan struct{})
		go func() {
			defer close(result.reminderScanDone)
			ticker := time.NewTicker(options.ReminderScanInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if service.restorePending.Load() {
						continue
					}
					service.maintenance.RLock()
					err := service.projectDueAutomations(options.Now().UTC())
					if err == nil {
						err = service.projectDueReminders(ctx)
					}
					if err == nil {
						err = service.projectDueClientFollowups(ctx)
					}
					if err == nil {
						err = service.projectDueTasks(ctx)
					}
					if err == nil {
						err = service.projectDueContentItems(ctx)
					}
					service.maintenance.RUnlock()
					if err != nil && options.Logger != nil && !errors.Is(err, context.Canceled) {
						options.Logger.Printf("due source scan failed: %v", err)
					}
					if err != nil && !errors.Is(err, context.Canceled) {
						service.recordRuntimeDatabaseFailure("due-source-scan")
					}
				}
			}
		}()
	}
	if options.DiskSpaceScanInterval > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		result.diskSpaceScanCancel = cancel
		result.diskSpaceScanDone = make(chan struct{})
		go func() {
			defer close(result.diskSpaceScanDone)
			ticker := time.NewTicker(options.DiskSpaceScanInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if service.restorePending.Load() {
						continue
					}
					service.maintenance.RLock()
					err := service.scanDiskSpace()
					service.maintenance.RUnlock()
					if err != nil && options.Logger != nil {
						options.Logger.Print("storage capacity check could not be completed safely")
					}
				}
			}
		}()
	}
	return result, nil
}

func (a *API) health(c *gin.Context) {
	sqlDB, err := a.db.DB()
	if err != nil || sqlDB.PingContext(c.Request.Context()) != nil {
		a.recordRuntimeDatabaseFailure(requestIDFromContext(c))
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
