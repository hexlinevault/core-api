package i18n

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v2"
)

type I18nConfig struct {
	Default      string
	LanguageMaps map[string]string
	Path         string
}
type localizationFile map[string]string

type Replacements map[string]interface{}

var (
	localizations = map[string]string{
		"en.validator.default":          "The %v is invalid",
		"en.validator.required":         "The %v is required",
		"en.validator.required_if":      "The %v is required when %v is %v",
		"en.validator.required_if_in":   "The %v is required when %v contains %v",
		"en.validator.required_without": "The %v is required when %v is empty",
		"en.validator.email":            "The %v must be a valid email address",
		"en.validator.eq":               "The %v must equal with %v",
		"en.validator.gt":               "The %v must greater than %v%v",
		"en.validator.gte":              "The %v must greater than or equal to %v%v",
		"en.validator.lt":               "The %v must less than %v%v",
		"en.validator.lte":              "The %v must less than or equal to %v%v",
		"en.validator.dive":             "The %v must be an array %v",
		"en.validator.oneof":            "The %v does not exist in %v",
		"en.validator.eqfield":          "The %v should be equal to %v",
		"en.validator.numeric":          "The %v should be a valid numeric",
		"en.validator.date":             "The %v date format should be format yyyy-MM-dd",
		"en.validator.datetime":         "The %v datetime format shoule be format yyyy-MM-dd H:i:s",
		"en.validator.date_range":       "The %v date range is invalid",
		"en.validator.regexp":           "The %v value pattern must be %v",
		"en.validator.min":              "The %v must not be least than %v%v",
		"en.validator.max":              "The %v must not be greater than %v%v",
		"en.validator.characters":       "characters",
		"en.validator.uri":              "The %v must be a valid URL",
	}
	languageMaps = map[string]string{}
	defaultLang  = "en"
)

const (
	jsonFileExt = ".json"
	yamlFileExt = ".yaml"
	ymlFileExt  = ".yml"
	tomlFileExt = ".toml"
	csvFileExt  = ".csv"
)

// New initialize language setup
// look at https://docs.microsoft.com/en-us/openspecs/office_standards/ms-oe376/6c085406-a698-4e12-9d4d-c3b0ee3dbc4a
// for maping language BCP 47 Code with directory such as
// map[string]string{"en-us": "en", "en-gb": "en"}

func New(conf I18nConfig) {
	defaultLang = conf.Default
	languageMaps = conf.LanguageMaps

	if files, err := getLocalizationFiles(conf.Path); err != nil {
		panic(err)
	} else {
		if err := generateLocalizations(files, conf.Path); err != nil {
			panic(err)
		}
	}
}

// T transalate language using context
func T(c context.Context, key string, replaces ...interface{}) string {
	_, formatStr, _, match := GetFormat(c, key)
	if !match {
		return formatStr
	}
	return applyReplacements(formatStr, replaces)
}

// TL transalate language using language key
func TL(lang, key string, replaces ...interface{}) string {
	c := &gin.Context{
		Request: &http.Request{
			Header: http.Header{
				"Accept-Language": []string{lang},
			},
		},
	}
	_, formatStr, _, match := GetFormat(c, key)
	if !match {
		return formatStr
	}
	return applyReplacements(formatStr, replaces)
}

func applyReplacements(formatStr string, replaces []interface{}) string {
	if len(replaces) == 0 {
		return formatStr
	}
	if vv, ok := replaces[0].(*Replacements); ok {
		return replace(formatStr, vv)
	}
	return fmt.Sprintf(formatStr, replaces...)
}

func ParseT(ct context.Context, key string, replaces ...interface{}) string {
	_, format, paramsCount, match := GetFormat(ct, key)
	if !match || paramsCount == 0 {
		return format
	} else {
		if vv, ok := replaces[0].(*Replacements); ok {
			return replace(format, vv)
		} else {
			placements := []interface{}{}
			for i, v := range replaces {
				if i < paramsCount {
					placements = append(placements, v)
				}
			}
			return fmt.Sprintf(format, placements...)
		}
	}
}

func GetLocale(c context.Context) string {
	lang := defaultLang
	if c != nil {
		if ct, ok := c.(*gin.Context); ok {
			if v, ok := ct.Request.Header["Accept-Language"]; ok {
				lang = v[0]
			}
		}
	}
	if lang != "en" {
		if v, ok := languageMaps[lang]; ok {
			lang = v
		} else {
			lang = defaultLang
		}
	}
	return lang
}

func GetFormat(c context.Context, key string) (string, string, int, bool) {
	lang := GetLocale(c)
	key = fmt.Sprintf("%s.%s", lang, key)
	if v, ok := localizations[key]; ok {
		variableCount := strings.Count(v, "%")
		return lang, v, variableCount, true
	}
	return lang, key, 0, false
}

func replace(str string, replacements ...*Replacements) string {
	b := &bytes.Buffer{}
	tmpl, err := template.New("").Parse(str)
	if err != nil {
		return str
	}

	replacementsMerge := Replacements{}
	for _, replacement := range replacements {
		for k, v := range *replacement {
			replacementsMerge[k] = v
		}
	}

	err = template.Must(tmpl, err).Execute(b, replacementsMerge)
	if err != nil {
		return str
	}
	buff := b.String()
	return buff
}

func generateLocalizations(files []string, path string) error {
	for _, file := range files {
		newLocalizations, err := getLocalizationsFromFile(file, path)
		if err != nil {
			return err
		}
		for key, value := range newLocalizations {
			SetLocalization(key, value)
		}
	}
	return nil
}

func SetLocalization(key, value string) {
	localizations[key] = value
}

func getLocalizationFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		ext := filepath.Ext(path)
		if !info.IsDir() && (ext == jsonFileExt || ext == yamlFileExt || ext == ymlFileExt || ext == tomlFileExt || ext == csvFileExt) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func getLocalizationsFromFile(file, path string) (map[string]string, error) {
	newLocalizations := map[string]string{}

	openFile, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	defer openFile.Close()
	byteValue, err := io.ReadAll(openFile)
	if err != nil {
		return nil, err
	}

	localizationFile := localizationFile{}
	ext := filepath.Ext(file)
	switch ext {
	case jsonFileExt:
		err = json.Unmarshal(byteValue, &localizationFile)
	case yamlFileExt, ymlFileExt:
		err = yaml.Unmarshal(byteValue, &localizationFile)
	case tomlFileExt:
		_, err = toml.Decode(string(byteValue), &localizationFile)
	case csvFileExt:
		err = parseCSV(byteValue, &localizationFile)
	default:
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	slicePath := getSlicePath(file, path)
	for key, value := range localizationFile {
		newLocalizations[strings.Join(append(slicePath, key), ".")] = value
	}

	return newLocalizations, nil
}

func parseCSV(value []byte, l *localizationFile) error {
	r := csv.NewReader(bytes.NewReader(value))
	localizations := localizationFile{}
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		localizations[record[0]] = record[1]
	}
	*l = localizations
	return nil
}

func getSlicePath(file, path string) []string {
	dir, file := filepath.Split(file)

	paths := strings.Replace(dir, path, "", -1)
	pathSlice := strings.Split(paths, string(filepath.Separator))

	var strs []string
	for _, part := range pathSlice {
		part := strings.TrimSpace(part)
		part = strings.Trim(part, "/")
		if part != "" {
			strs = append(strs, part)
		}
	}

	strs = append(strs, strings.Replace(file, filepath.Ext(file), "", -1))
	return strs
}
