package validators

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
)

func RequiredIfIn(fl validator.FieldLevel) bool {
	param := strings.Split(fl.Param(), `:`)
	paramField := param[0]
	paramValue := param[1]

	if paramField == `` {
		return true
	}

	// param field reflect.Value.
	var paramFieldValue reflect.Value

	if fl.Parent().Kind() == reflect.Ptr {
		paramFieldValue = fl.Parent().Elem().FieldByName(paramField)
	} else {
		paramFieldValue = fl.Parent().FieldByName(paramField)
	}
	vals := strings.Split(paramValue, " ")

	isIn := false
	for _, v := range vals {
		if isEq(paramFieldValue, v) {
			isIn = true
		}
	}
	if isIn {
		return hasValue(fl)
	}
	return true
}

// The following functions are copied from validator.v10 lib.

func hasValue(fl validator.FieldLevel) bool {
	return requireCheckFieldKind(fl, "")
}

func requireCheckFieldKind(fl validator.FieldLevel, param string) bool {
	field := fl.Field()
	if len(param) > 0 {
		if fl.Parent().Kind() == reflect.Ptr {
			field = fl.Parent().Elem().FieldByName(param)
		} else {
			field = fl.Parent().FieldByName(param)
		}
	}
	switch field.Kind() {
	case reflect.Slice, reflect.Map, reflect.Ptr, reflect.Interface, reflect.Chan, reflect.Func:
		return !field.IsNil()
	default:
		_, _, nullable := fl.ExtractType(field)
		if nullable && field.Interface() != nil {
			return true
		}
		return field.IsValid() && field.Interface() != reflect.Zero(field.Type()).Interface()
	}
}

func isEq(field reflect.Value, value string) bool {
	switch field.Kind() {
	case reflect.String:
		return field.String() == value

	case reflect.Slice, reflect.Map, reflect.Array:
		p, ok := asInt(value)
		if !ok {
			return false
		}
		return int64(field.Len()) == p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p, ok := asInt(value)
		if !ok {
			return false
		}
		return field.Int() == p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p, ok := asUint(value)
		if !ok {
			return false
		}
		return field.Uint() == p

	case reflect.Float32, reflect.Float64:
		p, ok := asFloat(value)
		if !ok {
			return false
		}
		return field.Float() == p
	}

	return false
}

func asInt(param string) (int64, bool) {
	i, err := strconv.ParseInt(param, 0, 64)
	return i, err == nil
}

func asUint(param string) (uint64, bool) {
	i, err := strconv.ParseUint(param, 0, 64)
	return i, err == nil
}

func asFloat(param string) (float64, bool) {
	i, err := strconv.ParseFloat(param, 64)
	return i, err == nil
}
