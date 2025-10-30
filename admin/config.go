package admin

const (
	DEFAULT_ADMIN_USERNAME    = "admin"
	DEFAULT_ADMIN_PASSWORD    = "admin.123"
	DEFAULT_AUTHENTICATOR_KEY = "00008e03308a-0b1f-0000-0628ba3cba87"
)

type Config struct {
	Administrator *Administrator `json:"administrator"`
}
