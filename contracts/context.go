package contracts

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/hexlinevault/core-api/bootstrap"
	"github.com/hexlinevault/core-api/constants"
	systementities "github.com/hexlinevault/core-api/entities/systems"
	"github.com/hexlinevault/core-api/errors"
	coreErrors "github.com/hexlinevault/core-api/errors"
	"github.com/hexlinevault/core-api/i18n"
	"github.com/hexlinevault/core-api/utils"
	"github.com/hexlinevault/core-api/validators"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	jwt "github.com/golang-jwt/jwt/v4"
	"github.com/oschwald/geoip2-golang"
	"github.com/redis/go-redis/v9"
	"github.com/thoas/go-funk"
)

type (
	// AppContext helper context
	AppContext[T any] struct {
		*gin.Context
		claims *systementities.CustomClaims[T]
		method string
		oauth  bootstrap.OAuth[T]
		geoip  bootstrap.GeoIP
	}
)

const (
	BASIC_SCHEMA   string = "Basic "
	BEARER_SCHEMA  string = "Bearer "
	msAudienceName string = "micro_service"

	tokenStatusRevoked = "revoked"
	tokenStatusBanned  = "banned"
)

var Validator *validator.Validate

var genericBracketsRe = regexp.MustCompile(`\[[^\]]*\]`)

// reflectStructField resolves a reflect.StructField for the failing field
// by traversing the struct type via StructNamespace. This replaces the
// fork-only ReflectStructField() method so we can use the upstream validator.
func reflectStructField(s interface{}, ve validator.FieldError) reflect.StructField {
	rawNs := genericBracketsRe.ReplaceAllString(ve.StructNamespace(), "")
	parts := strings.Split(rawNs, ".")
	if len(parts) < 2 {
		return reflect.StructField{}
	}
	parts = parts[1:] // strip leading struct type name

	t := reflect.TypeOf(s)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	var sf reflect.StructField
	for _, part := range parts {
		if t.Kind() != reflect.Struct {
			return reflect.StructField{}
		}
		var ok bool
		sf, ok = t.FieldByName(part)
		if !ok {
			return reflect.StructField{}
		}
		t = sf.Type
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
	}
	return sf
}

func init() {
	Validator = validator.New()
	Validator.RegisterTagNameFunc(func(fld reflect.StructField) string {
		if v := fld.Tag.Get("vfield"); v != "" {
			name := strings.SplitN(v, ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		}
		return "-"
	})

	Validator.RegisterValidation("required_if_in", validators.RequiredIfIn)
	Validator.RegisterValidation("regexp", validators.Regexp)
}

func NewAppContext[T any](c *gin.Context) *AppContext[T] {
	return &AppContext[T]{Context: c}
}

// RegisterValidation register validation
func RegisterValidation(tag string, fn validator.Func, callValidationEvenIfNull ...bool) error {
	return Validator.RegisterValidation(tag, fn, callValidationEvenIfNull...)
}

// Token get header access token
func (ctx *AppContext[T]) Token() (string, error) {
	if token := ctx.Context.GetHeader("Authorization"); token != "" {
		token = strings.TrimSpace(token)
		if token == "" || len(token) < 8 || (token[:7] != BEARER_SCHEMA) {
			return "", coreErrors.ErrInvalidToken
		}
		token = strings.TrimSpace(token[7:])
		return token, nil
	}
	return "", coreErrors.ErrTokenNotFound
}

// ClientIP get client ip address
func (ctx *AppContext[T]) ClientIP() *string {
	clientIP := ctx.GetHeader("X-Forwarded-For")
	return &clientIP
}

func (ctx *AppContext[T]) parseClientIPtoNet() (net.IP, error) {
	ipStr := ctx.ClientIP()
	if ipStr == nil || *ipStr == "" {
		return nil, errors.ErrIpNotFound
	}
	first := strings.SplitN(*ipStr, ",", 2)[0]
	return net.ParseIP(strings.TrimSpace(first)), nil
}

// GeoCity ip addr city detail
func (ctx *AppContext[T]) GeoCity() (*geoip2.City, error) {
	ip, err := ctx.parseClientIPtoNet()
	if err != nil {
		return nil, err
	}
	return ctx.geoip.DB().City(ip)
}

// GeoCountry ip addr country detail
func (ctx *AppContext[T]) GeoCountry() (*geoip2.Country, error) {
	ip, err := ctx.parseClientIPtoNet()
	if err != nil {
		return nil, err
	}
	return ctx.geoip.DB().Country(ip)
}

// Host from Header
func (ctx *AppContext[T]) Host() *string {
	host := ctx.GetHeader("Host")
	return &host
}

// Method from Header
func (ctx *AppContext[T]) Method() *string {
	method := ctx.GetHeader("Method")
	return &method
}

// VerifyToken verify bearer token
func (ctx *AppContext[T]) VerifyToken(tokenTypes ...string) (*string, *systementities.CustomClaims[T], error) {
	if token, err := ctx.Token(); err == nil {
		claims, method, err := ctx.oauth.VerifyJWT(token)
		if err != nil {
			return nil, claims, err
		}
		ctx.claims = claims
		ctx.method = method
		match := false
		if len(tokenTypes) > 0 {
			if tokenTypes[0] != "*" {
				for _, v := range tokenTypes {
					if claims.Type == v {
						match = true
						break
					}
				}
			} else {
				match = true
			}
		} else {
			match = true
		}
		if !match {
			return nil, claims, coreErrors.ErrInvalidTokenType
		}

		redisKey := fmt.Sprintf(constants.RDRevokeIndex, claims.ID)
		ct := ctx.Request.Context()
		val, err := contract.redis.DB("session").Get(ct, redisKey).Result()
		if err != nil && err != redis.Nil {
			return nil, claims, err
		} else if val == tokenStatusRevoked {
			return nil, claims, coreErrors.ErrTokenRevoked
		} else if val == tokenStatusBanned {
			return nil, nil, coreErrors.ErrTokenBanned
		}

		uid := claims.Subject

		return &uid, claims, nil
	} else {
		return nil, nil, coreErrors.ErrCannotVerifyToken
	}
}

func (ctx *AppContext[T]) VerifyClaimPayload(ct context.Context, payload *systementities.DefaultPayload, redis redis.UniversalClient) error {
	switch ctx.method {
	case jwt.SigningMethodHS256.Name, jwt.SigningMethodHS384.Name, jwt.SigningMethodHS512.Name:
		if payload == nil {
			return coreErrors.ErrInvalidToken
		}
		token, err := ctx.Token()
		if err != nil {
			return coreErrors.ErrInvalidToken
		}
		if v, err := redis.Get(ct, fmt.Sprintf(constants.RDJWTUser, payload.Uid)).Result(); err != nil || v != token {
			return coreErrors.ErrTokenRevoked
		}
	}
	return nil
}

// VerifyBasicAuth verify basic auth
func (ctx *AppContext[T]) VerifyBasicAuth() error {
	var err error
	var authHeader = ctx.Context.GetHeader("X-Authorization")

	str, err := base64.StdEncoding.DecodeString(authHeader[len(BASIC_SCHEMA):])
	if err != nil {
		return coreErrors.ErrDecryptBase64
	}
	creds := strings.Split(string(str), ":")
	if len(creds) != 2 {
		return coreErrors.ErrInvalidUsernameOrPassword
	}
	username := creds[0]
	password := creds[1]
	if username == os.Getenv("BASIC_AUTH_USERNAME") && password == os.Getenv("BASIC_AUTH_PASSWORD") {
		return nil
	} else {
		return coreErrors.ErrInvalidUsernameOrPassword
	}
}

// GetClaims get jwt claims
func (ctx *AppContext[T]) GetClaims() *systementities.CustomClaims[T] {
	return ctx.claims
}

// RequestKey get request key
func (ctx *AppContext[T]) RequestKey(item interface{}) []string {
	data := ctx.StructToMap(item, "json")
	return funk.Keys(data).([]string)
}

// ModelData wrap request key contain model column name
func (ctx *AppContext[T]) ModelData(model interface{}, req map[string]interface{}) map[string]interface{} {
	modelKeys := ctx.GetModelColumns(model)
	data := map[string]interface{}{}
	for i, v := range req {
		if funk.Contains(modelKeys, i) {
			data[i] = v
		}
	}
	return data
}

// StructToMap map struct value
func (ctx *AppContext[T]) StructToMap(item interface{}, tag string) map[string]interface{} {
	res := map[string]interface{}{}
	if item == nil {
		return res
	}
	v := reflect.TypeOf(item)
	reflectValue := reflect.ValueOf(item)
	reflectValue = reflect.Indirect(reflectValue)

	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	for i := 0; i < v.NumField(); i++ {
		fieldTag := v.Field(i).Tag.Get(tag)
		field := reflectValue.Field(i).Interface()
		if fieldTag != "" && fieldTag != "-" {
			if v.Field(i).Type.Kind() == reflect.Struct {
				res[fieldTag] = ctx.StructToMap(field, tag)
			} else {
				res[fieldTag] = field
			}
		}
	}
	return res
}

// GetModelColumns get column name from struct model (struct field must define column option in tag)
func (ctx *AppContext[T]) GetModelColumns(item interface{}) []string {
	res := []string{}
	if item == nil {
		return res
	}
	v := reflect.TypeOf(item)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	for i := 0; i < v.NumField(); i++ {
		tag := v.Field(i).Tag.Get("gorm")
		if tag != "" && tag != "-" {
			column := ""
			tagOpts := strings.Split(tag, ";")
			for _, vtag := range tagOpts {
				if strings.HasPrefix(vtag, "column:") {
					column = strings.TrimPrefix(vtag, "column:")
					break
				}
			}
			if column != "" && v.Field(i).Type.Kind() != reflect.Struct {
				res = append(res, column)
			}
		}
	}
	return res
}

// Validate validate request data and print response if have error
func (ctx *AppContext[T]) Validate(s interface{}, prefixNamespace ...string) error {
	err := Validator.Struct(s)
	if _, ok := err.(*validator.InvalidValidationError); ok {
		return err
	}
	if err == nil {
		return nil
	}

	errorsFields := make(map[string]*errors.ValidateErrors)
	for _, ve := range err.(validator.ValidationErrors) {
		fe := reflectStructField(s, ve)
		fieldName, keyName := ctx.buildValidationFieldName(s, ve, fe, prefixNamespace)
		if _, ok := errorsFields[fieldName]; !ok {
			errorsFields[fieldName] = &errors.ValidateErrors{FieldName: fieldName}
		}
		msg := ctx.getValidationMessage(ve, fe, keyName)
		errorsFields[fieldName].Message = append(errorsFields[fieldName].Message, msg)
	}
	return errors.NewValidateErrors(errorsFields)
}

func (ctx *AppContext[T]) buildValidationFieldName(s interface{}, ve validator.FieldError, fe reflect.StructField, prefixNamespace []string) (fieldName, keyName string) {
	rawNamespace := genericBracketsRe.ReplaceAllString(ve.Namespace(), "")
	rawStructNamespace := genericBracketsRe.ReplaceAllString(ve.StructNamespace(), "")
	vNamespace := strings.Split(rawNamespace, ".")
	vNamespace = vNamespace[1 : len(vNamespace)-1]
	vNamespace = append(prefixNamespace, vNamespace...)
	namespace := strings.Split(rawStructNamespace, ".")
	namespace = namespace[1 : len(namespace)-1]
	namespace = append(prefixNamespace, namespace...)

	nodePrefix := ""
	if len(namespace) > 0 {
		sub := make([]string, 0, len(namespace))
		for i, v := range namespace {
			if vv := vNamespace[i : i+1]; len(vv) > 0 && vv[0] != "-" {
				sub = append(sub, vv[0])
			} else {
				sub = append(sub, utils.SnakeCase(v))
			}
		}
		nodePrefix = strings.Join(sub, ".") + "."
	}

	name := utils.SnakeCase(ve.StructField())
	if field := ve.Field(); field != "-" {
		name = field
	}
	if v := fe.Tag.Get("v-prefix"); v != "" {
		nodePrefix = v
		if v == "-" {
			nodePrefix = ""
		}
	}

	fieldName = fmt.Sprintf("%s%s", nodePrefix, name)
	keyName = strings.ReplaceAll(name, "_", " ")
	if v := fe.Tag.Get("label-key"); v != "" {
		keyName = i18n.ParseT(ctx, v)
	}
	return
}

func (ctx *AppContext[T]) getValidationMessage(ve validator.FieldError, fe reflect.StructField, keyName string) string {
	if formatKey := fe.Tag.Get("validate-key"); formatKey != "" {
		return i18n.ParseT(ctx, formatKey, keyName)
	}

	value := ve.Param()
	charSuffix := func() string {
		if ve.Type().String() == "string" {
			return fmt.Sprintf(" %s", i18n.ParseT(ctx, "validator.characters"))
		}
		return ""
	}

	switch tag := ve.Tag(); tag {
	case "required":
		return i18n.ParseT(ctx, "validator.required", keyName)
	case "required_if":
		p := strings.Split(value, `:`)
		return i18n.ParseT(ctx, "validator.required_if", keyName, utils.SnakeCase(p[0]), p[1])
	case "required_if_in":
		p := strings.Split(value, `:`)
		return i18n.ParseT(ctx, "validator.required_if_in", keyName, utils.SnakeCase(p[0]), strings.ReplaceAll(p[1], " ", ", "))
	case "required_without":
		return i18n.ParseT(ctx, "validator.required_without", keyName, value)
	case "email":
		return i18n.ParseT(ctx, "validator.email", keyName)
	case "eq":
		return i18n.ParseT(ctx, "validator.eq", keyName, value)
	case "lt":
		return i18n.ParseT(ctx, "validator.lt", keyName, value, charSuffix())
	case "lte":
		return i18n.ParseT(ctx, "validator.lte", keyName, value, charSuffix())
	case "gt":
		return i18n.ParseT(ctx, "validator.gt", keyName, value, charSuffix())
	case "gte":
		return i18n.ParseT(ctx, "validator.gte", keyName, value, charSuffix())
	case "dive":
		return i18n.ParseT(ctx, "validator.dive", keyName, value)
	case "oneof":
		return i18n.ParseT(ctx, "validator.oneof", keyName, strings.Join(strings.Split(value, " "), ", "))
	case "eqfield":
		return i18n.ParseT(ctx, "validator.eqfield", keyName, value)
	case "numeric":
		return i18n.ParseT(ctx, "validator.numeric", keyName)
	case "date":
		return i18n.ParseT(ctx, "validator.date", keyName)
	case "datetime":
		return i18n.ParseT(ctx, "validator.datetime", keyName)
	case "regexp":
		return i18n.ParseT(ctx, "validator.regexp", keyName, value)
	case "min":
		return i18n.ParseT(ctx, "validator.min", keyName, value, charSuffix())
	case "max":
		return i18n.ParseT(ctx, "validator.max", keyName, value, charSuffix())
	default:
		formatKey := fmt.Sprintf("validator.%s", tag)
		_, _, paramsCount, match := i18n.GetFormat(ctx, formatKey)
		if !match {
			return i18n.ParseT(ctx, "validator.default", keyName)
		}
		params := []interface{}{keyName}
		if paramsCount > 1 {
			params = append(params, value)
		}
		return i18n.ParseT(ctx, formatKey, params...)
	}
}
