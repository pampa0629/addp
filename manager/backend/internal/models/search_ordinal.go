package models

type SearchDocumentOrdinal struct {
	IndexName     string `gorm:"primaryKey;size:128"`
	DocumentID    string `gorm:"primaryKey;size:128"`
	SearchOrdinal int64  `gorm:"not null"`
}

func (SearchDocumentOrdinal) TableName() string { return "manager.search_document_ordinals" }
