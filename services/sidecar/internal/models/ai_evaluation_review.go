package models

type AIEvaluationReview struct {
	ProviderConfigVersion         *int64 `gorm:"column:provider_config_version" json:"provider_config_version"`
	ID                            string `gorm:"column:id;primaryKey" json:"id"`
	ProviderIDSnapshot            string `gorm:"column:provider_id_snapshot" json:"provider_id_snapshot"`
	ProviderNameSnapshot          string `gorm:"column:provider_name_snapshot" json:"provider_name_snapshot"`
	ProviderModelSnapshot         string `gorm:"column:provider_model_snapshot" json:"provider_model_snapshot"`
	DatasetVersion                int    `gorm:"column:dataset_version" json:"dataset_version"`
	SuiteKey                      string `gorm:"column:suite_key" json:"suite_key"`
	ProviderVersionMin            int64  `gorm:"column:provider_version_min" json:"provider_version_min"`
	ProviderVersionMax            int64  `gorm:"column:provider_version_max" json:"provider_version_max"`
	GroupLastCompletedAt          string `gorm:"column:group_last_completed_at" json:"group_last_completed_at"`
	RunCount                      int64  `gorm:"column:run_count" json:"run_count"`
	TotalCases                    int64  `gorm:"column:total_cases" json:"total_cases"`
	PassedCases                   int64  `gorm:"column:passed_cases" json:"passed_cases"`
	FailedCases                   int64  `gorm:"column:failed_cases" json:"failed_cases"`
	OverallWilsonLowerBPS         int    `gorm:"column:overall_wilson_lower_bps" json:"overall_wilson_lower_bps"`
	MinimumCategory               string `gorm:"column:minimum_category" json:"minimum_category"`
	MinimumCategoryWilsonLowerBPS int    `gorm:"column:minimum_category_wilson_lower_bps" json:"minimum_category_wilson_lower_bps"`
	ReadinessStatus               string `gorm:"column:readiness_status" json:"readiness_status"`
	ReadinessReasonsJSON          string `gorm:"column:readiness_reasons" json:"-"`
	CriticalFailureCodesJSON      string `gorm:"column:critical_failure_codes" json:"-"`
	Decision                      string `gorm:"column:decision" json:"decision"`
	Reason                        string `gorm:"column:reason" json:"reason"`
	ReviewedByActorID             string `gorm:"column:reviewed_by_actor_id" json:"reviewed_by_actor_id"`
	ReviewedByActorNameSnapshot   string `gorm:"column:reviewed_by_actor_name_snapshot" json:"reviewed_by_actor_name_snapshot"`
	CreatedAt                     string `gorm:"column:created_at" json:"created_at"`
}

func (AIEvaluationReview) TableName() string { return "ai_evaluation_reviews" }
