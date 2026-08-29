package models

type RoadmapMilestone struct {
	ID                 string  `gorm:"column:id;primaryKey" json:"id"`
	Title              string  `gorm:"column:title" json:"title"`
	Description        *string `gorm:"column:description" json:"description"`
	Year               int     `gorm:"column:year" json:"year"`
	Quarter            int     `gorm:"column:quarter" json:"quarter"`
	TargetDate         string  `gorm:"column:target_date" json:"target_date"`
	Status             string  `gorm:"column:status" json:"status"`
	ManualOrder        int64   `gorm:"column:manual_order" json:"manual_order"`
	ArchivedFromStatus *string `gorm:"column:archived_from_status" json:"archived_from_status"`
	Version            int64   `gorm:"column:version" json:"version"`
	CreatedAt          string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          string  `gorm:"column:updated_at" json:"updated_at"`
}

func (RoadmapMilestone) TableName() string { return "roadmap_milestones" }

type RoadmapMilestoneProject struct {
	MilestoneID string `gorm:"column:milestone_id;primaryKey" json:"milestone_id"`
	ProjectID   string `gorm:"column:project_id;primaryKey" json:"project_id"`
	LinkedAt    string `gorm:"column:linked_at" json:"linked_at"`
}

func (RoadmapMilestoneProject) TableName() string { return "roadmap_milestone_projects" }
