package systementities

import (
	"github.com/golang-jwt/jwt/v4"
)

type (
	CustomClaims[T any] struct {
		jwt.RegisteredClaims
		Type    string `json:"type,omitempty"`
		Payload T      `json:"payload,omitempty"`
	}

	DefaultPayload struct {
		Uid  int `json:"uid"`
		Data any `json:"data,omitempty"`
	}
)
