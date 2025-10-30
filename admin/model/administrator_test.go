package model

import "testing"

func TestNewAdministrator(t *testing.T) {
	admin := NewAdministrator()

	token
	t.Log(admin.VerifyLoginUser([]byte("1af64a9d8f7ad5s3a2fd132asd132asd1f98a7sdf98as7df98s7da4f65a4sdf")))
}
