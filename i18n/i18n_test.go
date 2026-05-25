package i18n_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hexlinevault/core-api/i18n"

	"github.com/gin-gonic/gin"
	"gotest.tools/assert"
)

func TestMain(m *testing.T) {
	i18n.New(i18n.I18nConfig{
		Default: "en",
		LanguageMaps: map[string]string{
			"en-US": "en",
			"th-TH": "th",
		},
		Path: "../test_files/locales",
	})
}

func TestI18nKeyNotExists(t *testing.T) {
	assert.Equal(t, i18n.T(context.TODO(), "not.exists"), "en.not.exists")
}

func TestI18nStaticKeyEN(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := new(bytes.Buffer)
	w := httptest.NewRecorder()
	buf.ReadFrom(w.Body)
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/", buf)
	assert.Equal(t, i18n.T(c, "test.hello"), "Hello World")
}

func TestI18nStaticKeyTH(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := new(bytes.Buffer)
	w := httptest.NewRecorder()
	buf.ReadFrom(w.Body)
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/", buf)
	c.Request.Header.Set("Accept-Language", "th-TH")
	assert.Equal(t, i18n.T(c, "test.hello"), "สวัสดี World")
}

func TestI18nFormatString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := new(bytes.Buffer)
	w := httptest.NewRecorder()
	buf.ReadFrom(w.Body)
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/", buf)
	c.Request.Header.Set("Accept-Language", "th-TH")
	assert.Equal(t, i18n.T(c, "test.hello_f", "BWorld"), "สวัสดี BWorld")
}

func TestI18nFormatMultiString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := new(bytes.Buffer)
	w := httptest.NewRecorder()
	buf.ReadFrom(w.Body)
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/", buf)
	c.Request.Header.Set("Accept-Language", "th-TH")
	assert.Equal(t, i18n.T(c, "test.hello_f_multi", "BWorld", 1, 1.2), "สวัสดี BWorld 1 1.20")
}

func TestI18nReplacements(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := new(bytes.Buffer)
	w := httptest.NewRecorder()
	buf.ReadFrom(w.Body)
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/", buf)
	c.Request.Header.Set("Accept-Language", "th-TH")
	assert.Equal(t, i18n.T(c, "test.hello_key", &i18n.Replacements{
		"Name": "BWorld",
	}), "สวัสดี BWorld")
}

func TestI18nCustomLanguage(t *testing.T) {
	assert.Equal(t, i18n.TL("th-TH", "test.hello"), "สวัสดี World")
	assert.Equal(t, i18n.TL("en-US", "test.hello"), "Hello World")
}
