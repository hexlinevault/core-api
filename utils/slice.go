package utils

func Shift[T any](s []T) (T, []T) {
	if len(s) == 0 {
		// Handle the case when the slice is empty.
		// Depending on your use case, you might want to return an error or a zero value.
		var zero T // Default zero value for type T
		return zero, s
	}
	// Return the first element and the slice without the first element
	return s[0], s[1:]
}

// FilterSlice takes a slice and returns a new slice containing only the elements that satisfy the given predicate function.
// T is the type of the elements in the slice.
// The predicate function takes an element of type T and returns true if the element should be included in the result, false otherwise.
func FilterSlice[T any](slice []T, predicate func(T) bool) []T {
	if slice == nil {
		return nil
	}
	result := make([]T, 0)
	for _, v := range slice {
		if predicate(v) {
			result = append(result, v)
		}
	}
	return result
}

// FindSlice takes a slice and returns the first element that satisfies the given predicate function.
// T is the type of the elements in the slice.
// The predicate function takes an element of type T and returns true if the element should be included in the result, false otherwise.
func FindSlice[T any](slice []T, predicate func(T) bool) T {
	for _, v := range slice {
		if predicate(v) {
			return v
		}
	}
	var zero T
	return zero
}
