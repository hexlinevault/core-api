package contracts_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hexlinevault/core-api/bootstrap"
	"github.com/hexlinevault/core-api/contracts"
	"github.com/hexlinevault/core-api/errors"
	"github.com/hexlinevault/core-api/helpers/dump"
	"github.com/hexlinevault/core-api/i18n"

	"github.com/gin-gonic/gin"
	tassert "github.com/stretchr/testify/assert"
	"gotest.tools/assert"
)

func TestValidate(t *testing.T) {
	type (
		istruct struct {
			Sub string `validate:"required" vfield:"ohlala"`
		}
		nested struct {
			NestedA int `validate:"required" v-prefix:"testcustomfield."`
			NestedB int `validate:"required" v-prefix:"-"`
		}
		a struct {
			nested
			A string  `test:"banana"`
			B string  `validate:"required_if_in=A:55 44" vfield:"bboy" test:"b"`
			C string  `validate:"omitempty,oneof=a b" vfield:"cname" test:"c"`
			D string  `validate:"required,min=5"`
			E string  `validate:"required" vfield:"elephant_dt"`
			F int     `validate:"min=5"`
			G int     `validate:"gte=5"`
			H string  `validate:"required" vfield:"TESTUPPER"`
			I istruct `vfield:"banana"`
			J istruct
			K int    `validate:"gtfield=F"`
			L int    `validate:"ltfield=F"`
			M string `validate:"required" label-key:"test"`
			N string `validate:"required" validate-key:"validator.custom" label-key:"sugarbaby"`
		}
	)
	label := "just a variable"
	i18n.SetLocalization("en.test", label)
	i18n.SetLocalization("en.validator.custom", "how are you %s?")
	i18n.SetLocalization("en.sugarbaby", "Sugar Baby")
	ct := contracts.AppContext[any]{}
	if err := ct.Validate(&a{
		A: "55",
		C: "D",
		D: "C",
	}); err != nil {
		e := err.(*errors.Error)
		dump.DD(e.ValidateErrors)
		assert.Equal(t, int(e.Code), 422)
		tassert.Contains(t, e.ValidateErrors, "TESTUPPER")
		assert.Equal(t, e.ValidateErrors["d"].Message[0], "The d must not be least than 5 characters")
		assert.Equal(t, e.ValidateErrors["f"].Message[0], "The f must not be least than 5")
		assert.Equal(t, e.ValidateErrors["g"].Message[0], "The g must greater than or equal to 5")
		assert.Equal(t, e.ValidateErrors["bboy"].Message[0], "The bboy is required when a contains 55, 44")
		assert.Equal(t, e.ValidateErrors["cname"].Message[0], "The cname does not exist in a, b")
		assert.Equal(t, e.ValidateErrors["m"].Message[0], fmt.Sprintf("The %s is required", label))
		assert.Equal(t, e.ValidateErrors["n"].Message[0], "how are you Sugar Baby?")
	}
}

func newTestContext() *contracts.AppContext[any] {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/", nil)
	return contracts.NewAppContext[any](c)
}

func TestValidateGenericType(t *testing.T) {
	type PmpSetting struct {
		AgentID     string `validate:"required" vfield:"agent_id"`
		AgentKey    string `validate:"required" vfield:"agent_key"`
		APIEndpoint string `validate:"required" vfield:"api_endpoint"`
	}
	type DepositWithdrawSetting struct {
		Enabled bool       `validate:"required" vfield:"enabled"`
		Pmp     PmpSetting `validate:"required"`
	}
	type ConfigurationType[T any] struct {
		Key   string `validate:"required" vfield:"key"`
		Value T      `validate:"required"`
	}

	ct := newTestContext()
	err := ct.Validate(&ConfigurationType[DepositWithdrawSetting]{})
	tassert.NotNil(t, err)

	e := err.(*errors.Error)
	tassert.Equal(t, 422, int(e.Code))

	// field names must not contain module path or generic type brackets
	for fieldName := range e.ValidateErrors {
		tassert.NotContains(t, fieldName, "stand-eleven", "field name should not contain module path")
		tassert.NotContains(t, fieldName, "[", "field name should not contain generic brackets")
		tassert.NotContains(t, fieldName, "]", "field name should not contain generic brackets")
	}

	// expected clean field paths
	tassert.Contains(t, e.ValidateErrors, "key")
	tassert.Contains(t, e.ValidateErrors, "value.enabled")
	tassert.Contains(t, e.ValidateErrors, "value.pmp.agent_id")
	tassert.Contains(t, e.ValidateErrors, "value.pmp.agent_key")
	tassert.Contains(t, e.ValidateErrors, "value.pmp.api_endpoint")
}

func TestOAuth(t *testing.T) {
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.8TLPbKjmE0uGLQyLnfHx2z-zy6G8qu5zFFXRSuJID_Y"
	os.Setenv("HMAC_SECRET", "secretkey")
	if _, _, err := new(bootstrap.OAuth[any]).VerifyJWT(token); err != nil {
		fmt.Println(err)
	}
}

// func TestVerifyPermission(t *testing.T) {
// 	os.Setenv("REDIS_DB", "0")
// 	os.Setenv("REDIS_HOST", "localhost:6379")
// 	rdDatabase, _ := strconv.Atoi(os.Getenv("REDIS_DB"))
// 	rdDB := bootstrap.CreateRedisConnection(&configs.RedisConn{
// 		UniversalOptions: redis.UniversalOptions{
// 			Addrs:    strings.Split(os.Getenv("REDIS_HOST"), ","),
// 			Password: os.Getenv("REDIS_PASSWORD"),
// 			DB:       rdDatabase,
// 		},
// 	})
// 	defer rdDB.Close()

// 	publicKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(`-----BEGIN PUBLIC KEY-----
// MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAu1SU1LfVLPHCozMxH2Mo
// 4lgOEePzNm0tRgeLezV6ffAt0gunVTLw7onLRnrq0/IzW7yWR7QkrmBL7jTKEn5u
// +qKhbwKfBstIs+bMY2Zkp18gnTxKLxoS2tFczGkPLPgizskuemMghRniWaoLcyeh
// kd3qqGElvW/VDL5AaWTg0nLVkjRo9z+40RQzuVaE8AkAFmxZzow3x+VJYKdjykkJ
// 0iT9wCS0DRTXu269V264Vf/3jvredZiKRkgwlL9xNAwxXFg0x/XFw005UWVRIkdg
// cKWTjpBP2dPwVZ4WWC+9aGVd+Gyn1o0CLelf4rEjGoXbAAEgAqeGUxrcIlbjXfbc
// mwIDAQAB
// -----END PUBLIC KEY-----`))
// 	if err != nil {
// 		t.Error(err)
// 	}
// 	bootstrap.RSAPublicKey = publicKey
// 	gin.SetMode(gin.TestMode)
// 	buf := new(bytes.Buffer)
// 	w := httptest.NewRecorder()
// 	buf.ReadFrom(w.Body)
// 	c, _ := gin.CreateTestContext(w)
// 	c.Request, _ = http.NewRequest("POST", "/", buf)
// 	c.Request.Header.Set("Authorization", "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWUsImlhdCI6MTUxNjIzOTAyMn0.BjYVpAJkPflykSwD1aYJHt_7WwDUeKLSuyhOO1tB9j7x-PltrLfyOkd8fXimFQJNHQgeZXX-lfc7mLUMQXJKchehg7TBvyOmBi_nbY8Q2i9_aJMZi8_dvfM8XuiUWNw3WP3FSH6_dwZ9P3PBOV9iZqoQFbLKF4P48CE2EQqk3PnNIUeMxU-w6pjfYZHsl1B-wF8N6_9rztiMnKqreo3Dg9N45NoWxgOEwSxHM2W5gqSkWxWox1V5B-ol-2f5TzxlQ5neD9D6L5emhGJHc_UtbXj347H4p4WMhjORIYGt9Ifyhg8tvNIhPmCWKyMOkUY3I6VkjH1LVTHwMvJOlPQVuQ")

// ctx := contracts.NewAppContext[appcontracts.ClaimPayload](c)// 	if _, _, err := ctx.VerifyToken(); err != nil {
// 		t.Error(err)
// 	} else {
// 		index := fmt.Sprintf(constants.RDUserPermissionsIndex, "1")
// 		rdDB.Del(c, index)
// 		if _, err := ctx.VerifyPermission("test"); err != nil {
// 			fmt.Println("response ", err)
// 			assert.Equal(t, errors.ErrPermissionNotFound, err)
// 		}
// 		if _, err := rdDB.Set(c, index, `["delete-item"]`, 0).Result(); err != nil {
// 			t.Error("fail to set index")
// 		}
// 		permission, err := ctx.VerifyPermission("delete-item")
// 		assert.Equal(t, permission, true)
// 		assert.Equal(t, err, nil)
// 	}
// }

// func TestRSA256JWT(t *testing.T) {
// 	os.Setenv("REDIS_DB", "0")
// 	os.Setenv("REDIS_HOST", "localhost:6379")
// 	rdDatabase, _ := strconv.Atoi(os.Getenv("REDIS_DB"))
// 	rdDB := bootstrap.CreateRedisConnection(&configs.RedisConn{
// 		UniversalOptions: redis.UniversalOptions{
// 			Addrs:    strings.Split(os.Getenv("REDIS_HOST"), ","),
// 			Password: os.Getenv("REDIS_PASSWORD"),
// 			DB:       rdDatabase,
// 		},
// 	})
// 	defer rdDB.Close()

// 	publicKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(`-----BEGIN PUBLIC KEY-----
// MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAu1SU1LfVLPHCozMxH2Mo
// 4lgOEePzNm0tRgeLezV6ffAt0gunVTLw7onLRnrq0/IzW7yWR7QkrmBL7jTKEn5u
// +qKhbwKfBstIs+bMY2Zkp18gnTxKLxoS2tFczGkPLPgizskuemMghRniWaoLcyeh
// kd3qqGElvW/VDL5AaWTg0nLVkjRo9z+40RQzuVaE8AkAFmxZzow3x+VJYKdjykkJ
// 0iT9wCS0DRTXu269V264Vf/3jvredZiKRkgwlL9xNAwxXFg0x/XFw005UWVRIkdg
// cKWTjpBP2dPwVZ4WWC+9aGVd+Gyn1o0CLelf4rEjGoXbAAEgAqeGUxrcIlbjXfbc
// mwIDAQAB
// -----END PUBLIC KEY-----`))
// 	if err != nil {
// 		t.Error(err)
// 	}
// 	bootstrap.RSAPublicKey = publicKey
// 	gin.SetMode(gin.TestMode)
// 	buf := new(bytes.Buffer)
// 	w := httptest.NewRecorder()
// 	buf.ReadFrom(w.Body)
// 	c, _ := gin.CreateTestContext(w)
// 	c.Request, _ = http.NewRequest("POST", "/", buf)
// 	c.Request.Header.Set("Authorization", "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWUsImlhdCI6MTUxNjIzOTAyMn0.BjYVpAJkPflykSwD1aYJHt_7WwDUeKLSuyhOO1tB9j7x-PltrLfyOkd8fXimFQJNHQgeZXX-lfc7mLUMQXJKchehg7TBvyOmBi_nbY8Q2i9_aJMZi8_dvfM8XuiUWNw3WP3FSH6_dwZ9P3PBOV9iZqoQFbLKF4P48CE2EQqk3PnNIUeMxU-w6pjfYZHsl1B-wF8N6_9rztiMnKqreo3Dg9N45NoWxgOEwSxHM2W5gqSkWxWox1V5B-ol-2f5TzxlQ5neD9D6L5emhGJHc_UtbXj347H4p4WMhjORIYGt9Ifyhg8tvNIhPmCWKyMOkUY3I6VkjH1LVTHwMvJOlPQVuQ")

// ctx := contracts.NewAppContext[appcontracts.ClaimPayload](c)// 	_, claim, err := ctx.VerifyToken()
// 	assert.Equal(t, err, nil)
// 	err = ctx.VerifyClaimPayload(c, claim.Payload, rdDB)
// 	assert.Equal(t, err, nil)
// }

// func TestVerifyClaimPayload(t *testing.T) {
// 	os.Setenv("REDIS_DB", "0")
// 	os.Setenv("REDIS_HOST", "localhost:6379")
// 	os.Setenv("HMAC_SECRET", "test")
// 	rdDatabase, _ := strconv.Atoi(os.Getenv("REDIS_DB"))
// 	rdDB := bootstrap.CreateRedisConnection(&configs.RedisConn{
// 		UniversalOptions: redis.UniversalOptions{
// 			Addrs:    strings.Split(os.Getenv("REDIS_HOST"), ","),
// 			Password: os.Getenv("REDIS_PASSWORD"),
// 			DB:       rdDatabase,
// 		},
// 	})
// 	defer rdDB.Close()
// 	gin.SetMode(gin.TestMode)
// 	buf := new(bytes.Buffer)
// 	w := httptest.NewRecorder()
// 	buf.ReadFrom(w.Body)
// 	c, _ := gin.CreateTestContext(w)
// 	c.Request, _ = http.NewRequest("POST", "/", buf)
// 	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJwYXlsb2FkIjp7InVpZCI6MX0sImlhdCI6MTY1NTcwMzg3M30.k0ftWcsAdpKpd5S7kQ6IgdLBfHOYIDwqNEF5Wz-4s_8"
// 	rdDB.Set(c, fmt.Sprintf(constants.RDJWTUser, 1), token, 0)
// 	c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

// ctx := contracts.NewAppContext[appcontracts.ClaimPayload](c)// 	if _, claim, err := ctx.VerifyToken(); err != nil {
// 		t.Error(err)
// 	} else {
// 		err := ctx.VerifyClaimPayload(c, claim.Payload, rdDB)
// 		assert.Equal(t, err, nil)
// 	}
// }

// func TestVerifyClaimPayloadRevoked(t *testing.T) {
// 	os.Setenv("REDIS_DB", "0")
// 	os.Setenv("REDIS_HOST", "localhost:6379")
// 	os.Setenv("HMAC_SECRET", "test")
// 	rdDatabase, _ := strconv.Atoi(os.Getenv("REDIS_DB"))
// 	rdDB := bootstrap.CreateRedisConnection(&configs.RedisConn{
// 		UniversalOptions: redis.UniversalOptions{
// 			Addrs:    strings.Split(os.Getenv("REDIS_HOST"), ","),
// 			Password: os.Getenv("REDIS_PASSWORD"),
// 			DB:       rdDatabase,
// 		},
// 	})
// 	defer rdDB.Close()
// 	gin.SetMode(gin.TestMode)
// 	buf := new(bytes.Buffer)
// 	w := httptest.NewRecorder()
// 	buf.ReadFrom(w.Body)
// 	c, _ := gin.CreateTestContext(w)
// 	c.Request, _ = http.NewRequest("POST", "/", buf)
// 	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJwYXlsb2FkIjp7InVpZCI6MX0sImlhdCI6MTY1NTcwMzg3M30.k0ftWcsAdpKpd5S7kQ6IgdLBfHOYIDwqNEF5Wz-4s_8"
// 	rdDB.Del(c, fmt.Sprintf(constants.RDJWTUser, 1))
// 	c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

// ctx := contracts.NewAppContext[appcontracts.ClaimPayload](c)// 	if _, claim, err := ctx.VerifyToken(); err != nil {
// 		t.Error(err)
// 	} else {
// 		err := ctx.VerifyClaimPayload(c, claim.Payload, rdDB)
// 		assert.Equal(t, err, errors.ErrTokenRevoked)
// 	}
// }
