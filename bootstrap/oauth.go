package bootstrap

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"errors"
	"os"
	"time"

	systementities "github.com/hexlinevault/core-api/entities/systems"
	coreErrors "github.com/hexlinevault/core-api/errors"

	"github.com/golang-jwt/jwt/v4"
)

type (
	// OAuth oauth struct funcs
	OAuth[T any] struct {
	}

	tokenType int
)

var (
	RSAPrivateKey        *rsa.PrivateKey
	RSAPublicKey         *rsa.PublicKey
	ECDSAPrivateKey      *ecdsa.PrivateKey
	ECDSAPublicKey       *ecdsa.PublicKey
	Ed25519PrivateKey    *crypto.PrivateKey
	Ed25519PublicKey     *crypto.PublicKey
	AccessTokenDuration  = time.Duration(time.Hour * 24 * 14)
	RefreshTokenDuration = time.Duration(time.Hour * 24 * 20)
)

// Grant Type
const (
	_                               = iota
	TokenTypeRefreshToken tokenType = iota
	TokenTypeAccessToken
	GrantTypePassword     = "password"
	GrantTypeRefreshToken = "refresh_token"
)

func CreateOAuthCredential() {
	pwd, _ := os.Getwd()
	{
		key, err := os.ReadFile(pwd + "/storage/private.key")
		if err != nil {
			Logger(context.Background()).WithError(err).WithField("component", "oauth").Fatal("Failed to read private key")
		}
		switch os.Getenv("JWT_ALGORITHM") {
		case "ECDSA":
			ECDSAPrivateKey, err = jwt.ParseECPrivateKeyFromPEM(key)
		case "Ed25519":
			*Ed25519PrivateKey, err = jwt.ParseEdPrivateKeyFromPEM(key)
		default:
			RSAPrivateKey, err = jwt.ParseRSAPrivateKeyFromPEM(key)
		}
		if err != nil {
			Logger(context.Background()).WithError(err).WithField("component", "oauth").Fatal("Failed to parse private key")
		}
	}
	{
		key, err := os.ReadFile(pwd + "/storage/public.key")
		if err != nil {
			Logger(context.Background()).WithError(err).WithField("component", "oauth").Fatal("Failed to read public key")
		}
		switch os.Getenv("JWT_ALGORITHM") {
		case "ECDSA":
			ECDSAPublicKey, err = jwt.ParseECPublicKeyFromPEM(key)
		case "Ed25519":
			*Ed25519PublicKey, err = jwt.ParseEdPublicKeyFromPEM(key)
		default:
			RSAPublicKey, err = jwt.ParseRSAPublicKeyFromPEM(key)
		}
		if err != nil {
			Logger(context.Background()).WithError(err).WithField("component", "oauth").Fatal("Failed to parse public key")
		}
	}
}

// VerifyJWT verify jwt token
func (ctl OAuth[T]) VerifyJWT(t string) (*systementities.CustomClaims[T], string, error) {
	tok, err := jwt.ParseWithClaims(t, &systementities.CustomClaims[T]{}, func(token *jwt.Token) (interface{}, error) {
		// Check is token use correct signing method
		switch token.Method.(type) {
		case *jwt.SigningMethodRSA, *jwt.SigningMethodRSAPSS:
			return RSAPublicKey, nil
		case *jwt.SigningMethodECDSA:
			return ECDSAPrivateKey, nil
		case *jwt.SigningMethodEd25519:
			return Ed25519PrivateKey, nil
		case *jwt.SigningMethodHMAC:
			return []byte(os.Getenv("HMAC_SECRET")), nil
		default:
			return nil, errors.New("invalid token type")
		}
		// return secret for this signing method
	})
	var claims *systementities.CustomClaims[T]
	var validClaim bool = false
	var method string = ""
	if err != nil {
		return nil, method, err
	} else {
		if tok != nil && tok.Valid {
			claims, validClaim = tok.Claims.(*systementities.CustomClaims[T])
			method = tok.Method.Alg()
			if validClaim {
				return claims, method, nil
			}
		}
		return nil, method, coreErrors.ErrInvalidToken
	}
}
