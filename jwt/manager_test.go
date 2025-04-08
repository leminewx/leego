package jwt

import (
	"testing"
	"time"
)

func TestManager_Generate(t *testing.T) {
	manager := NewManager(ALG_HS256, []byte("1234567890qwertyuiopasdfghjklzxcvbnm"), 7*24*time.Hour)
	sign, err := manager.Generate(&StandardClaims{})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(sign)
}
