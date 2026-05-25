package utils

import (
	"fmt"
	"reflect"
	"strings"
)

// fieldValueForKey returns the value to use as a map key, dereferencing pointers (e.g. *string -> string).
// If the field is a nil pointer, the returned value is nil.
func fieldValueForKey(fval reflect.Value) any {
	v := fval
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	return v.Interface()
}

// ConvertMap converts a slice of struct pointers to a map.
// K: The type of the map key (must be comparable).
// T: The type of the struct.
// req: Slice of pointers to structs to be converted.
// fields: One or more field names from the struct to be used as the key.
//
// If multiple fields are provided, their values are converted to strings and joined with "_".
// If a single field is provided, its original type is preserved if it matches K.
// Pointer fields (e.g. *string) are dereferenced for the key; nil pointers are skipped.
// It handles nil pointers and invalid field names gracefully.
func ConvertMap[K comparable, T any](req []*T, fields ...string) map[K]*T {
	data := make(map[K]*T)
	// Return empty map if no key fields are specified
	if len(fields) == 0 {
		return data
	}

	for _, v := range req {
		// Skip nil elements in the slice
		if v == nil {
			continue
		}

		val := reflect.ValueOf(v)
		// If it's a pointer, get the element it points to
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}

		// Only process structs
		if val.Kind() != reflect.Struct {
			continue
		}

		var keyVal any
		if len(fields) > 1 {
			// Combine multiple fields into a string key
			var parts []string
			for _, f := range fields {
				fval := val.FieldByName(f)
				if fval.IsValid() {
					parts = append(parts, fmt.Sprintf("%v", fieldValueForKey(fval)))
				}
			}
			keyVal = strings.Join(parts, "_")
		} else {
			// Use a single field value as the key
			fval := val.FieldByName(fields[0])
			if fval.IsValid() {
				keyVal = fieldValueForKey(fval)
			}
		}

		// Skip if the key could not be determined
		if keyVal == nil {
			continue
		}

		// Attempt to assign to map with type assertion
		if k, ok := keyVal.(K); ok {
			data[k] = v
		} else {
			// Fallback: If K is string, convert the key value to string format
			strKey := fmt.Sprintf("%v", keyVal)
			if k, ok := any(strKey).(K); ok {
				data[k] = v
			}
		}
	}
	return data
}

// Omit takes a map or struct and returns a map[string]any with the specified keys removed.
// For structs, it uses the "json" tag for key names if available.
func Omit(v any, keys ...string) map[string]any {
	result := make(map[string]any)
	if v == nil {
		return result
	}

	omitMap := make(map[string]struct{})
	for _, k := range keys {
		omitMap[k] = struct{}{}
	}

	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return result
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Map:
		for _, key := range val.MapKeys() {
			keyStr := fmt.Sprintf("%v", key.Interface())
			if _, ok := omitMap[keyStr]; !ok {
				result[keyStr] = val.MapIndex(key).Interface()
			}
		}

	case reflect.Struct:
		processStruct(val, result, func(name string) bool {
			_, ok := omitMap[name]
			return !ok
		})
	}

	return result
}

// Pick takes a map or struct and returns a map[string]any with only the specified keys included.
// For structs, it uses the "json" tag for key names if available.
func Pick(v any, keys ...string) map[string]any {
	result := make(map[string]any)
	if v == nil {
		return result
	}

	pickMap := make(map[string]struct{})
	for _, k := range keys {
		pickMap[k] = struct{}{}
	}

	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return result
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Map:
		for _, key := range val.MapKeys() {
			keyStr := fmt.Sprintf("%v", key.Interface())
			if _, ok := pickMap[keyStr]; ok {
				result[keyStr] = val.MapIndex(key).Interface()
			}
		}

	case reflect.Struct:
		processStruct(val, result, func(name string) bool {
			_, ok := pickMap[name]
			return ok
		})
	}

	return result
}

// processStruct iterates over struct fields and applies a filter to add them to the result map.
// It handles embedded structs by flattening them, matching standard JSON marshaling behavior.
func processStruct(val reflect.Value, result map[string]any, filter func(string) bool) {
	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}

		fieldVal := val.Field(i)
		jsonTag := field.Tag.Get("json")
		name := field.Name
		isFlatten := field.Anonymous && jsonTag == ""

		if jsonTag != "" {
			if jsonTag == "-" {
				continue
			}
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				name = parts[0]
				isFlatten = false
			}
		}

		if isFlatten {
			fv := fieldVal
			for fv.Kind() == reflect.Ptr {
				if fv.IsNil() {
					break
				}
				fv = fv.Elem()
			}
			if fv.Kind() == reflect.Struct {
				processStruct(fv, result, filter)
				continue
			}
		}

		if filter(name) {
			result[name] = fieldVal.Interface()
		}
	}
}

// OmitSlice takes a slice or array of maps/structs and returns a slice of map[string]any with specified keys removed.
func OmitSlice(v any, keys ...string) []map[string]any {
	result := make([]map[string]any, 0)
	if v == nil {
		return result
	}

	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return result
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
		return result
	}

	for i := 0; i < val.Len(); i++ {
		result = append(result, Omit(val.Index(i).Interface(), keys...))
	}

	return result
}

// PickSlice takes a slice or array of maps/structs and returns a slice of map[string]any with only specified keys included.
func PickSlice(v any, keys ...string) []map[string]any {
	result := make([]map[string]any, 0)
	if v == nil {
		return result
	}

	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return result
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
		return result
	}

	for i := 0; i < val.Len(); i++ {
		result = append(result, Pick(val.Index(i).Interface(), keys...))
	}

	return result
}

// Map applies a function to each element of a slice, returning a slice of the results.
// T is the type of the elements in the input slice.
// U is the type of the elements in the output slice.
func Map[T any, U any](slice []T, f func(T) U) []U {
	if slice == nil {
		return nil
	}
	result := make([]U, len(slice))
	for i, v := range slice {
		result[i] = f(v)
	}
	return result
}
