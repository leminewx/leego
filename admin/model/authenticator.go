package model

import (
	"sync"

	"gitee.com/leminewx/gokit/ggid"
	"gitee.com/leminewx/leego/middleware/authenticator"
	"gitee.com/leminewx/leego/middleware/authenticator/jwt"
)

type Authenticator struct {
	*authenticator.Authenticator

	max    int
	tokens *sync.Map
}

func NewAuthenticator(max int) *Authenticator {
	return &Authenticator{
		max:    max,
		tokens: new(sync.Map),
		Authenticator: authenticator.
			NewHS256Authenticator("", []byte("00008e03308a-0b1f-0000-0628ba3cba87")).
			WithSkipPaths("/auth/pubkey", "/auth/login", "/api/v1/metrics/base").
			WithExceptedIssuer("leego").
			WithExceptedAudience("leego_admin").
			WithExpiresAfterSeconds(3600 * 24).
			WithValidateStandardClaims(true),
	}
}

func (own *Authenticator) GenerateToken() (string, error) {
	token := own.Authenticator.GenerateToken(&jwt.StandardClaims{Id: ggid.New().GetShort()})
	tokenBytes, err := own.Authenticator.Sign(token)

}
