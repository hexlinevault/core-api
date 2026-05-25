package utils

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// GetNestedValue retrieves a value from a nested structure (struct, map, slice) using a dot-separated path.
// It returns the value and an error if the path is invalid, a field is missing, or types are incompatible.
func GetNestedValue[T any](obj any, path string) (T, error) {
	var zero T
	if obj == nil {
		return zero, fmt.Errorf("object is nil")
	}

	parts := strings.Split(path, ".")
	current := reflect.ValueOf(obj)

	for i, part := range parts {
		// Dereference pointer/interface 
		for current.Kind() == reflect.Ptr || current.Kind() == reflect.Interface {
			if current.IsNil() {
				return zero, fmt.Errorf("property '%s' is nil at path part '%s'", part, strings.Join(parts[:i], "."))
			}
			current = current.Elem()
		}

		switch current.Kind() {
		case reflect.Struct:
			field := current.FieldByName(part)
			if !field.IsValid() {
				return zero, fmt.Errorf("field '%s' not found in struct at path '%s'", part, strings.Join(parts[:i], "."))
			}
			current = field

		case reflect.Map:
			keyType := current.Type().Key()
			key := reflect.ValueOf(part)
			// Try to convert key if it's not directly assignable
			if !key.Type().AssignableTo(keyType) {
				if i, err := strconv.Atoi(part); err == nil {
					v := reflect.ValueOf(i)
					if v.Type().ConvertibleTo(keyType) {
						key = v.Convert(keyType)
					}
				}
			}

			// Final check before MapIndex to avoid panic
			if !key.Type().AssignableTo(keyType) {
				return zero, fmt.Errorf("invalid key type '%s' for map at path '%s' (expected %s)", part, strings.Join(parts[:i], "."), keyType)
			}

			val := current.MapIndex(key)
			if !val.IsValid() {
				return zero, fmt.Errorf("key '%s' not found in map at path '%s'", part, strings.Join(parts[:i], "."))
			}
			current = val

		case reflect.Slice, reflect.Array:
			index, err := strconv.Atoi(part)
			if err != nil {
				return zero, fmt.Errorf("invalid slice index '%s' at path '%s'", part, strings.Join(parts[:i], "."))
			}
			if index < 0 || index >= current.Len() {
				return zero, fmt.Errorf("slice index '%d' out of bounds at path '%s' (length %d)", index, strings.Join(parts[:i], "."), current.Len())
			}
			current = current.Index(index)

		default:
			return zero, fmt.Errorf("cannot access property '%s' on kind %s at path '%s'", part, current.Kind(), strings.Join(parts[:i], "."))
		}
	}

	// Final dereference and type checking
	for {
		if current.IsValid() {
			if val, ok := current.Interface().(T); ok {
				return val, nil
			}
		}

		if current.Kind() == reflect.Ptr || current.Kind() == reflect.Interface {
			if current.IsNil() {
				return zero, fmt.Errorf("final value at path '%s' is nil", path)
			}
			current = current.Elem()
		} else {
			break
		}
	}

	// Try flexible conversion
	if current.IsValid() {
		targetType := reflect.TypeOf((*T)(nil)).Elem()
		if current.Type().ConvertibleTo(targetType) {
			converted := current.Convert(targetType)
			if val, ok := converted.Interface().(T); ok {
				return val, nil
			}
		}
		return zero, fmt.Errorf("cannot convert %s to %s at path '%s'", current.Type(), targetType, path)
	}

	return zero, fmt.Errorf("value not found at path '%s'", path)
}

// NestedValue retrieves a value from a nested structure.
// It returns the fallback value if any error occurs (invalid path, nil, type mismatch).
func NestedValue[T any](obj any, path string, fallback T) T {
	val, err := GetNestedValue[T](obj, path)
	if err != nil {
		return fallback
	}

	// Check if returned value is nil (for pointer types, maps, slices, etc.)
	v := reflect.ValueOf(val)
	if v.IsValid() {
		switch v.Kind() {
		case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
			if v.IsNil() {
				return fallback
			}
		}
	} else {
		return fallback
	}

	return val
}
