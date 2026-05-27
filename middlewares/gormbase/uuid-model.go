package gormbase

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type UUIDModel struct {
	ID        uuid.UUID `gorm:"primarykey;type:varchar(50)" json:"id" db:"id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func (m *UUIDModel) BeforeCreate(tx *gorm.DB) error {
	tx.Statement.SetColumn("ID", uuid.Must(uuid.NewV7()))
	return nil
}
