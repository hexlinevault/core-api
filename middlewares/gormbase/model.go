package gormbase

import (
	"time"

	"gorm.io/gorm"
)

type Model struct {
	ID        uint64    `gorm:"primarykey" json:"id" db:"id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

type DeleteTimestamp struct {
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at" db:"deleted_at"`
}
