package model

import (
	"sync"

	"gitee.com/leminewx/gokit/ggid"
	jwt2 "gitee.com/leminewx/leego/middleware/jwt"
)

type Authenticator struct {
	*jwt2.Authenticator

	max    int
	tokens *sync.Map
}

func NewAuthenticator(max int) *Authenticator {
	return &Authenticator{
		max:    max,
		tokens: new(sync.Map),
		Authenticator: jwt2.NewHS256Authenticator("", []byte("00008e03308a-0b1f-0000-0628ba3cba87")).
			WithSkipPaths("/auth/pubkey", "/auth/login", "/api/v1/metrics/base").
			WithExceptedIssuer("leego").
			WithExceptedAudience("leego_admin").
			WithExpiresAfterSeconds(3600 * 24).
			WithValidateStandardClaims(true),
	}
}

func (own *Authenticator) GenerateToken() (string, error) {
	token := own.Authenticator.GenerateToken(&jwt2.StandardClaims{Id: ggid.New().GetShort()})
	tokenBytes, err := own.Authenticator.Sign(token)

}
