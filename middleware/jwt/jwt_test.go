package jwt

import "time"

type customizedClaims struct {
	*StandardClaims

	Username string `json:"username"`
	Team     string `json:"team"`
	Role     string `json:"role"`
}

var (
	testStandardClaims = &StandardClaims{
		Id:        "id",
		Issuer:    "issuer",
		Subject:   "subject",
		Audiences: []string{"audience"},
		IssuedAt:  time.Now().Unix(),
		NotBefore: time.Now().Add(-time.Hour).Unix(),
		ExpiresAt: time.Now().Add(+time.Hour).Unix(),
	}

	testCustomizedClaims = &customizedClaims{
		StandardClaims: &StandardClaims{
			Id:        "id",
			Issuer:    "issuer",
			Subject:   "subject",
			Audiences: []string{"audience"},
			IssuedAt:  time.Now().Unix(),
			ExpiresAt: time.Now().Add(time.Hour * 24).Unix(),
			NotBefore: time.Now().Unix(),
		},
		Username: "username",
		Team:     "team",
		Role:     "role",
	}
)
