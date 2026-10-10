package models

import "time"

// ContentIndexOutlet is a durable fence, not evidence of external deletion.
type ContentIndexOutlet struct {
	IndexName  string `gorm:"primaryKey;size:128"`
	Epoch      string `gorm:"not null;size:36"`
	EndpointID string `gorm:"not null;size:64"`
	Configured bool   `gorm:"not null"`
	Isolated   bool   `gorm:"not null"`
	UpdatedAt  time.Time
}

func (ContentIndexOutlet) TableName() string { return "manager.content_index_outlets" }

// ContentIndexDelivery deliberately contains no document payload or credential.
type ContentIndexDelivery struct {
	ID              string `gorm:"primaryKey;size:36"`
	IndexName       string `gorm:"not null;size:128;index:idx_content_delivery_pending,priority:1;uniqueIndex:idx_content_delivery_task,priority:1"`
	TenantID        int64  `gorm:"not null;index;check:content_delivery_tenant,tenant_id > 0"`
	DocumentID      string `gorm:"not null;size:128"`
	Kind            string `gorm:"not null;size:16;check:content_delivery_kind,kind IN ('write','delete','purge')"`
	Filter          string `gorm:"not null;type:text"`
	EndpointID      string `gorm:"not null;size:64;uniqueIndex:idx_content_delivery_task,priority:2"`
	TaskCorrelation string `gorm:"not null;default:'';size:36;check:content_delivery_correlation,task_correlation = '' OR task_correlation = id"`
	Status          string `gorm:"not null;size:16;index:idx_content_delivery_pending,priority:2;check:content_delivery_status,status IN ('queued','submitting','submitted','unknown','succeeded','failed','canceled')"`
	TaskUID         *int64 `gorm:"uniqueIndex:idx_content_delivery_task,priority:3;check:content_delivery_receipt,((status IN ('queued','submitting','unknown') AND task_uid IS NULL AND task_enqueued_at = '') OR (status IN ('submitted','succeeded','failed','canceled') AND task_uid IS NOT NULL AND task_uid >= 0 AND task_enqueued_at <> ''))"`
	TaskEnqueuedAt  string `gorm:"not null;size:40"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (ContentIndexDelivery) TableName() string { return "manager.content_index_deliveries" }
