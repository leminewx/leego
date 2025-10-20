package authenticator

import (
	"strings"
	"testing"
)

func TestNewDefaultConfig(t *testing.T) {
	name := "test-config"
	config := NewDefaultConfig(name)

	if config == nil {
		t.Fatal("NewDefaultConfig returned nil")
	}

	if config.Name != name {
		t.Errorf("Expected name %s, got %s", name, config.Name)
	}

	if config.Id == "" {
		t.Error("Expected non-empty ID")
	}

	if config.Scope != "" {
		t.Errorf("Expected empty scope, got %s", config.Scope)
	}

	if config.SkipAbsolutePathMap == nil {
		t.Error("SkipAbsolutePathMap should be initialized")
	}

	if config.SkipWildcardPathSlice == nil {
		t.Error("SkipWildcardPathSlice should be initialized")
	}

	if config.JWTStandardClaimsConfig == nil {
		t.Fatal("JWTStandardClaimsConfig should be initialized")
	}

	if config.Validation != 1 {
		t.Errorf("Expected Validation=1, got %d", config.Validation)
	}

	if config.ExpiresAfterSeconds != 24*3600 {
		t.Errorf("Expected ExpiresAfterSeconds=86400, got %d", config.ExpiresAfterSeconds)
	}
}

func TestAuthenticator_WithScope(t *testing.T) {
	auth := &Authenticator{Config: &Config{JWTStandardClaimsConfig: &JWTStandardClaimsConfig{}}}
	scope := "test-scope"

	auth.SetScope(scope)
	if auth.Scope != scope {
		t.Errorf("Expected scope %s, got %s", scope, auth.Scope)
	}
}

func TestAuthenticator_WithExceptedIssuer(t *testing.T) {
	auth := &Authenticator{Config: &Config{JWTStandardClaimsConfig: &JWTStandardClaimsConfig{}}}
	issuer := "test-issuer"

	result := auth.WithExceptedIssuer(issuer)

	if result != auth {
		t.Error("WithExceptedIssuer should return the same authenticator instance")
	}

	if auth.ExceptedIssuer != strings.TrimSpace(issuer) {
		t.Errorf("Expected issuer %s, got %s", issuer, auth.ExceptedIssuer)
	}

	// Test with spaces
	auth.WithExceptedIssuer("  issuer-with-spaces  ")
	if auth.ExceptedIssuer != "issuer-with-spaces" {
		t.Errorf("Expected trimmed issuer, got %s", auth.ExceptedIssuer)
	}
}

func TestAuthenticator_WithExceptedAudience(t *testing.T) {
	auth := &Authenticator{Config: &Config{JWTStandardClaimsConfig: &JWTStandardClaimsConfig{}}}
	audience := "test-audience"

	result := auth.WithExceptedAudience(audience)

	if result != auth {
		t.Error("WithExceptedAudience should return the same authenticator instance")
	}

	if auth.ExceptedAudience != strings.TrimSpace(audience) {
		t.Errorf("Expected audience %s, got %s", audience, auth.ExceptedAudience)
	}
}

func TestAuthenticator_WithNotBeforeSeconds(t *testing.T) {
	auth := &Authenticator{Config: &Config{JWTStandardClaimsConfig: &JWTStandardClaimsConfig{}}}

	// Test positive value
	seconds := int64(300)
	result := auth.WithNotBeforeSeconds(seconds)

	if result != auth {
		t.Error("WithNotBeforeSeconds should return the same authenticator instance")
	}

	if auth.NotBeforeSeconds != seconds {
		t.Errorf("Expected NotBeforeSeconds=%d, got %d", seconds, auth.NotBeforeSeconds)
	}

	// Test zero value (should not update)
	auth.WithNotBeforeSeconds(0)
	if auth.NotBeforeSeconds != seconds {
		t.Errorf("NotBeforeSeconds should not change when setting zero value, expected %d, got %d", seconds, auth.NotBeforeSeconds)
	}

	// Test negative value (should not update)
	auth.WithNotBeforeSeconds(-100)
	if auth.NotBeforeSeconds != seconds {
		t.Errorf("NotBeforeSeconds should not change when setting negative value, expected %d, got %d", seconds, auth.NotBeforeSeconds)
	}
}

func TestAuthenticator_WithExpiresAfterSeconds(t *testing.T) {
	auth := &Authenticator{Config: &Config{JWTStandardClaimsConfig: &JWTStandardClaimsConfig{}}}

	// Test positive value
	seconds := int64(3600)
	result := auth.WithExpiresAfterSeconds(seconds)

	if result != auth {
		t.Error("WithExpiresAfterSeconds should return the same authenticator instance")
	}

	if auth.ExpiresAfterSeconds != seconds {
		t.Errorf("Expected ExpiresAfterSeconds=%d, got %d", seconds, auth.ExpiresAfterSeconds)
	}

	// Test zero value (should not update)
	auth.WithExpiresAfterSeconds(0)
	if auth.ExpiresAfterSeconds != seconds {
		t.Errorf("ExpiresAfterSeconds should not change when setting zero value, expected %d, got %d", seconds, auth.ExpiresAfterSeconds)
	}

	// Test negative value (should not update)
	auth.WithExpiresAfterSeconds(-100)
	if auth.ExpiresAfterSeconds != seconds {
		t.Errorf("ExpiresAfterSeconds should not change when setting negative value, expected %d, got %d", seconds, auth.ExpiresAfterSeconds)
	}
}

func TestAuthenticator_WithSkipPaths(t *testing.T) {
	auth := &Authenticator{
		Config: &Config{
			SkipAbsolutePathMap:   make(map[string]struct{}),
			SkipWildcardPathSlice: make([]string, 0),
		},
	}

	paths := []string{
		"/api/health",
		"/static/*",
		"/public/*",
		"/docs",
		"  ", // empty path with spaces
		"",   // empty path
	}

	result := auth.WithSkipPaths(paths...)

	if result != auth {
		t.Error("WithSkipPaths should return the same authenticator instance")
	}

	// Check absolute paths
	if _, exists := auth.SkipAbsolutePathMap["/api/health"]; !exists {
		t.Error("Expected /api/health in SkipAbsolutePathMap")
	}
	if _, exists := auth.SkipAbsolutePathMap["/docs"]; !exists {
		t.Error("Expected /docs in SkipAbsolutePathMap")
	}

	// Check wildcard paths
	if len(auth.SkipWildcardPathSlice) != 2 {
		t.Errorf("Expected 2 wildcard paths, got %d", len(auth.SkipWildcardPathSlice))
	}

	expectedWildcards := []string{"/static", "/public"}
	for _, expected := range expectedWildcards {
		found := false
		for _, actual := range auth.SkipWildcardPathSlice {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected %s in SkipWildcardPathSlice", expected)
		}
	}

	// Verify empty paths are skipped
	if len(auth.SkipAbsolutePathMap) != 2 {
		t.Errorf("Expected 2 absolute paths, got %d", len(auth.SkipAbsolutePathMap))
	}
}

func TestAuthenticator_Sync(t *testing.T) {
	auth := &Authenticator{
		Config: &Config{
			SkipAbsolutePathMap:   make(map[string]struct{}),
			SkipWildcardPathSlice: []string{"/old"},
			JWTStandardClaimsConfig: &JWTStandardClaimsConfig{
				Validation:          1,
				NotBeforeSeconds:    100,
				ExpiresAfterSeconds: 200,
				ExceptedIssuer:      "old-issuer",
				ExceptedAudience:    "old-audience",
			},
		},
	}

	config := &Config{
		SkipAbsolutePathMap: map[string]struct{}{
			"/api/health": {},
			"/docs":       {},
		},
		SkipWildcardPathSlice: []string{"/static", "/public"},
		JWTStandardClaimsConfig: &JWTStandardClaimsConfig{
			Validation:          0,
			NotBeforeSeconds:    300,
			ExpiresAfterSeconds: 400,
			ExceptedIssuer:      "new-issuer",
			ExceptedAudience:    "new-audience",
		},
	}

	auth.Sync(config)

	// Test path synchronization
	if len(auth.SkipAbsolutePathMap) != 2 {
		t.Errorf("Expected 2 absolute paths after sync, got %d", len(auth.SkipAbsolutePathMap))
	}
	if _, exists := auth.SkipAbsolutePathMap["/api/health"]; !exists {
		t.Error("Expected /api/health in SkipAbsolutePathMap after sync")
	}
	if _, exists := auth.SkipAbsolutePathMap["/docs"]; !exists {
		t.Error("Expected /docs in SkipAbsolutePathMap after sync")
	}

	if len(auth.SkipWildcardPathSlice) != 2 {
		t.Errorf("Expected 2 wildcard paths after sync, got %d", len(auth.SkipWildcardPathSlice))
	}
	if auth.SkipWildcardPathSlice[0] != "/static" || auth.SkipWildcardPathSlice[1] != "/public" {
		t.Errorf("Unexpected wildcard paths after sync: %v", auth.SkipWildcardPathSlice)
	}

	// Test JWT config synchronization
	if auth.Validation != 0 {
		t.Errorf("Expected Validation=0 after sync, got %d", auth.Validation)
	}
	if auth.NotBeforeSeconds != 300 {
		t.Errorf("Expected NotBeforeSeconds=300 after sync, got %d", auth.NotBeforeSeconds)
	}
	if auth.ExpiresAfterSeconds != 400 {
		t.Errorf("Expected ExpiresAfterSeconds=400 after sync, got %d", auth.ExpiresAfterSeconds)
	}
	if auth.ExceptedIssuer != "new-issuer" {
		t.Errorf("Expected ExceptedIssuer=new-issuer after sync, got %s", auth.ExceptedIssuer)
	}
	if auth.ExceptedAudience != "new-audience" {
		t.Errorf("Expected ExceptedAudience=new-audience after sync, got %s", auth.ExceptedAudience)
	}
}

func TestAuthenticator_Sync_WithNilJWTConfig(t *testing.T) {
	auth := &Authenticator{
		Config: &Config{
			JWTStandardClaimsConfig: &JWTStandardClaimsConfig{
				Validation: 1,
			},
		},
	}

	config := &Config{
		SkipAbsolutePathMap:   make(map[string]struct{}),
		SkipWildcardPathSlice: []string{},
		// JWTStandardClaimsConfig is nil
	}

	auth.Sync(config)

	if auth.JWTStandardClaimsConfig == nil {
		t.Error("JWTStandardClaimsConfig should not be nil after sync with nil config")
	}
}
