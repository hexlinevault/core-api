package utils

import "reflect"

// Pointer returns a pointer to the value passed in.
func Pointer[T any](v T) *T {
	return &v
}

// Value returns the value of the pointer or the zero value of T if the pointer is nil.
func Value[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// ValueOr returns the value of the pointer or the fallback value if the pointer is nil.
func ValueOr[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}

// CloneDeep returns a fully independent deep copy of *T.
// Handles nested pointers, slices, maps, and structs recursively.
// Types with unexported fields (e.g. time.Time) are shallow-copied safely.
func CloneDeep[T any](p *T) *T {
	if p == nil {
		return nil
	}
	return cloneValue(reflect.ValueOf(p)).Interface().(*T)
}

func cloneValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		cp := reflect.New(v.Type().Elem())
		cp.Elem().Set(cloneValue(v.Elem()))
		return cp

	case reflect.Struct:
		cp := reflect.New(v.Type()).Elem()
		hasUnexported := false
		for i := 0; i < v.NumField(); i++ {
			if !cp.Field(i).CanSet() {
				hasUnexported = true
				break
			}
		}
		if hasUnexported {
			cp.Set(v)
			return cp
		}
		for i := 0; i < v.NumField(); i++ {
			cp.Field(i).Set(cloneValue(v.Field(i)))
		}
		return cp

	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		cp := reflect.MakeSlice(v.Type(), v.Len(), v.Cap())
		for i := 0; i < v.Len(); i++ {
			cp.Index(i).Set(cloneValue(v.Index(i)))
		}
		return cp

	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		cp := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			cp.SetMapIndex(cloneValue(iter.Key()), cloneValue(iter.Value()))
		}
		return cp

	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		inner := cloneValue(v.Elem())
		cp := reflect.New(v.Type()).Elem()
		cp.Set(inner)
		return cp

	case reflect.Array:
		cp := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			cp.Index(i).Set(cloneValue(v.Index(i)))
		}
		return cp

	default:
		cp := reflect.New(v.Type()).Elem()
		cp.Set(v)
		return cp
	}
}
