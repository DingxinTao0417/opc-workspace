package database

import (
	"fmt"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	seedClientID  = "018f0000-0000-7000-8000-000000000001"
	seedProjectID = "018f0000-0000-7000-8000-000000000002"
	seedTaskOneID = "018f0000-0000-7000-8000-000000000101"
	seedTaskTwoID = "018f0000-0000-7000-8000-000000000102"
)

func SeedDevelopmentData(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var taskCount int64
		if err := tx.Model(&models.Task{}).Count(&taskCount).Error; err != nil {
			return fmt.Errorf("count tasks before seed: %w", err)
		}
		if taskCount != 0 {
			return nil
		}

		now := time.Now().UTC()
		today := now.Format("2006-01-02")
		timestamp := now.Format(time.RFC3339Nano)
		if err := tx.Exec(`
			INSERT OR IGNORE INTO clients(id, name, contact_name, email, status, created_at, updated_at)
			VALUES (?, 'Acme Studio', 'Mia Chen', 'mia@example.test', 'active', ?, ?)
		`, seedClientID, timestamp, timestamp).Error; err != nil {
			return fmt.Errorf("seed client: %w", err)
		}
		if err := tx.Exec(`
			INSERT OR IGNORE INTO projects(id, name, description, client_id, status, amount_minor, color, created_at, updated_at)
			VALUES (?, '品牌网站改版', 'Development-only sample project', ?, 'in_progress', 2400000, '#5E6AD2', ?, ?)
		`, seedProjectID, seedClientID, timestamp, timestamp).Error; err != nil {
			return fmt.Errorf("seed project: %w", err)
		}

		estimate50 := 50
		estimate90 := 90
		order1 := 1
		order2 := 2
		dueSoon := now.Add(4 * time.Hour).Format(time.RFC3339Nano)
		tasks := []models.Task{
			{
				ID: seedTaskOneID, Title: "完成首页交互评审",
				Description: "确认关键状态和交互细节。", Kind: "work", Status: "in_progress", Priority: "P1",
				ProjectID: stringPointer(seedProjectID), DueDate: &dueSoon, PlannedDate: &today,
				EstimatedMinutes: &estimate50, ManualOrder: &order1, Version: 1, CreatedAt: timestamp, UpdatedAt: timestamp,
			},
			{
				ID: seedTaskTwoID, Title: "整理客户跟进清单",
				Description: "Development-only sample task.", Kind: "work", Status: "todo", Priority: "P2",
				PlannedDate: &today, EstimatedMinutes: &estimate90, ManualOrder: &order2,
				Version: 1, CreatedAt: timestamp, UpdatedAt: timestamp,
			},
		}
		if err := tx.Create(&tasks).Error; err != nil {
			return fmt.Errorf("seed tasks: %w", err)
		}
		return nil
	})
}

func stringPointer(value string) *string { return &value }
