package utils

import (
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func DownloadImage(url, filePath string) error {
	// Send HTTP GET request to the image URL
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// Create the output file
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Copy the response body to the file
	_, err = io.Copy(file, response.Body)
	if err != nil {
		return err
	}

	return nil
}

func CreateDirectory(directoryPath string) error {
	// Check if the directory already exists
	_, err := os.Stat(directoryPath)
	if os.IsNotExist(err) {
		// Directory does not exist, so create it
		err := os.MkdirAll(directoryPath, 0755)
		if err != nil {
			return err
		}
	} else if err != nil {
		// An error occurred while checking the directory existence
		return err
	}

	return nil
}

func MsToTime(ms string) (time.Time, error) {
	msInt, err := strconv.ParseInt(ms, 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(0, msInt*int64(time.Millisecond)), nil
}

var camel = regexp.MustCompile("(^[^A-Z]*|[A-Z]*)([A-Z][^A-Z]+|$)")

// SnakeCase convert string to snake case
func SnakeCase(s string) string {
	var a []string
	for _, sub := range camel.FindAllStringSubmatch(s, -1) {
		if sub[1] != "" {
			a = append(a, sub[1])
		}
		if sub[2] != "" {
			a = append(a, sub[2])
		}
	}
	return strings.ToLower(strings.Join(a, "_"))
}

func BoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// MaskText shows the first `prefix` characters and masks the rest with '*'.
// e.g. MaskText("johndoe123", 5) → "johnd*****"
func MaskText(text string, prefix int) string {
	runes := []rune(text)
	if len(runes) <= prefix {
		return text
	}
	return string(runes[:prefix]) + strings.Repeat("*", len(runes)-prefix)
}
