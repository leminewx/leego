package authorizator

import "gitee.com/leminewx/leego/middleware/authorizator/jwt"

type Config struct {
	Id        string        `json:"id" xml:"id"`
	Name      string        `json:"name" xml:"name"`
	Scope     string        `json:"scope" xml:"socpe"`
	Algorithm jwt.Algorithm `json:"algorithm" xml:"algorithm"`
}
