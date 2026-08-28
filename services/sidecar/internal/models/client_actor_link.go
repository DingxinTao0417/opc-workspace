package models

type ClientActorLink struct {
	ID                string  `gorm:"column:id;primaryKey" json:"id"`
	ClientID          string  `gorm:"column:client_id" json:"client_id"`
	ActorID           string  `gorm:"column:actor_id" json:"actor_id"`
	Role              string  `gorm:"column:role" json:"role"`
	LinkedByActorID   string  `gorm:"column:linked_by_actor_id" json:"linked_by_actor_id"`
	LinkedAt          string  `gorm:"column:linked_at" json:"linked_at"`
	UnlinkedAt        *string `gorm:"column:unlinked_at" json:"unlinked_at"`
	UnlinkedByActorID *string `gorm:"column:unlinked_by_actor_id" json:"unlinked_by_actor_id"`
	UnlinkReason      *string `gorm:"column:unlink_reason" json:"unlink_reason"`
}

func (ClientActorLink) TableName() string { return "client_actor_links" }
