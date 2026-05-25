package gorm

import (
	"strings"

	"gorm.io/gorm"
)

// IsGormError check gorm error
func IsGormError(err error) bool {
	if err == gorm.ErrRecordNotFound ||
		err == gorm.ErrInvalidTransaction ||
		err == gorm.ErrNotImplemented ||
		err == gorm.ErrMissingWhereClause ||
		err == gorm.ErrUnsupportedRelation ||
		err == gorm.ErrPrimaryKeyRequired ||
		err == gorm.ErrModelValueRequired ||
		err == gorm.ErrInvalidData ||
		err == gorm.ErrUnsupportedDriver ||
		err == gorm.ErrRegistered ||
		err == gorm.ErrInvalidField ||
		strings.HasPrefix(err.Error(), "mysql:") {
		return true
	}
	return false
}
