package bootstrap

// resolveConnectionName returns the first name provided or "default".
func resolveConnectionName(names []string) string {
	if len(names) > 0 && names[0] != "" {
		return names[0]
	}
	return "default"
}
