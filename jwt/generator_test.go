package jwt

import (
	"errors"
	"log"
	"testing"
	"time"
)

func TestGenerator(t *testing.T) {
	generator := NewDefaultGenerator([]byte("123qwe!@#QWE"))
	sign, err := generator.Encode(
		NewPayload().
			WithAud("service-user-001").
			WithExp(time.Now().Unix() + 20).
			WithNbf(time.Now().Unix() + 10).
			WithSub("lemine"),
	)
	if err != nil {
		t.Fatal(err)
	}
	log.Println("sign ===>>>", sign)

	payload, err := generator.Decode(sign)
	if err != nil {
		t.Fatal(err)
	}
	log.Println("payload ===>>>", payload)

	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		if err := generator.Validate(sign); err != nil {
			if errors.Is(err, errNBFNotEffective) {
				log.Println("ineffective token")
				continue
			}
			t.Error(err)
			break
		}
		log.Println("valid token")
	}
	log.Println("invalid token")
}
