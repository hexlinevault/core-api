package validators

import (
	"regexp"
	"sync"

	"github.com/go-playground/validator/v10"
)

var regexpCache sync.Map

// Regexp regular expression validation
func Regexp(fl validator.FieldLevel) bool {
	pattern := fl.Param()
	var re *regexp.Regexp
	if v, ok := regexpCache.Load(pattern); ok {
		re = v.(*regexp.Regexp)
	} else {
		re = regexp.MustCompile(pattern)
		regexpCache.Store(pattern, re)
	}
	return re.MatchString(fl.Field().String())
}
