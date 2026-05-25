package utils

import (
	"html/template"
	"os"
	"time"
)

var FuncMaps = template.FuncMap{
	"now": func(t string) string { // ex { getEnv "2016-01-02" }
		return time.Now().Format(t)
	},
	"time": func(t time.Time, f string) string { // ex { getEnv "2016-01-02" }
		return t.Format(f)
	},
	"debug": func() bool {
		return os.Getenv("APP_DEBUG") == "true"
	},
	"getEnv": func(env string) string { // ex { getEnv "ENV_NAME" }
		return os.Getenv(env)
	},
}
