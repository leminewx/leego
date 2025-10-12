package jwt

type Header struct {
	Type      string    `json:"typ"`
	Algorithm Algorithm `json:"alg"`
}
