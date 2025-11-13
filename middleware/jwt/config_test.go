package jwt

import (
	"strings"
	"testing"
)

func TestNewDefaultConfig(t *testing.T) {
	config := NewDefaultConfig()

	if config == nil {
		t.Fatal("NewDefaultConfig returned nil")
	}

	if config.Id == "" {
		t.Error("Expected non-empty ID")
	}

	if config.Scope != "" {
		t.Errorf("Expected empty scope, got %s", config.Scope)
	}

	if config.SkipAbsolutePaths == nil {
		t.Error("SkipAbsolutePaths should be initialized")
	}

	if config.SkipWildcardPaths == nil {
		t.Error("SkipWildcardPaths should be initialized")
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
	auth := &Authenticator{config: &Config{JWTStandardClaimsConfig: &JWTStandardClaimsConfig{}}}
	scope := "test-scope"

	auth.SetScope(scope)
	if auth.config.Scope != scope {
		t.Errorf("Expected scope %s, got %s", scope, auth.config.Scope)
	}
}

func TestAuthenticator_WithExceptedIssuer(t *testing.T) {
	auth := &Authenticator{config: &Config{JWTStandardClaimsConfig: &JWTStandardClaimsConfig{}}}
	issuer := "test-issuer"

	result := auth.WithExceptedIssuer(issuer)

	if result != auth {
		t.Error("WithExceptedIssuer should return the same authenticator instance")
	}

	if auth.config.ExceptedIssuer != strings.TrimSpace(issuer) {
		t.Errorf("Expected issuer %s, got %s", issuer, auth.config.ExceptedIssuer)
	}

	// Test with spaces
	auth.WithExceptedIssuer("  issuer-with-spaces  ")
	if auth.config.ExceptedIssuer != "issuer-with-spaces" {
		t.Errorf("Expected trimmed issuer, got %s", auth.config.ExceptedIssuer)
	}
}

func TestAuthenticator_WithExceptedAudience(t *testing.T) {
	auth := &Authenticator{config: &Config{JWTStandardClaimsConfig: &JWTStandardClaimsConfig{}}}
	audience := "test-audience"

	result := auth.WithExceptedAudience(audience)

	if result != auth {
		t.Error("WithExceptedAudience should return the same authenticator instance")
	}

	if auth.config.ExceptedAudience != strings.TrimSpace(audience) {
		t.Errorf("Expected audience %s, got %s", audience, auth.config.ExceptedAudience)
	}
}

func TestAuthenticator_WithNotBeforeSeconds(t *testing.T) {
	auth := &Authenticator{config: &Config{JWTStandardClaimsConfig: &JWTStandardClaimsConfig{}}}

	// Test positive value
	seconds := int64(300)
	result := auth.WithNotBeforeSeconds(seconds)

	if result != auth {
		t.Error("WithNotBeforeSeconds should return the same authenticator instance")
	}

	if auth.config.NotBeforeSeconds != seconds {
		t.Errorf("Expected NotBeforeSeconds=%d, got %d", seconds, auth.config.NotBeforeSeconds)
	}

	// Test zero value (should not update)
	auth.WithNotBeforeSeconds(0)
	if auth.config.NotBeforeSeconds != seconds {
		t.Errorf("NotBeforeSeconds should not change when setting zero value, expected %d, got %d", seconds, auth.config.NotBeforeSeconds)
	}

	// Test negative value (should not update)
	auth.WithNotBeforeSeconds(-100)
	if auth.config.NotBeforeSeconds != seconds {
		t.Errorf("NotBeforeSeconds should not change when setting negative value, expected %d, got %d", seconds, auth.config.NotBeforeSeconds)
	}
}

func TestAuthenticator_WithExpiresAfterSeconds(t *testing.T) {
	auth := &Authenticator{config: &Config{JWTStandardClaimsConfig: &JWTStandardClaimsConfig{}}}

	// Test positive value
	seconds := int64(3600)
	result := auth.WithExpiresAfterSeconds(seconds)

	if result != auth {
		t.Error("WithExpiresAfterSeconds should return the same authenticator instance")
	}

	if auth.config.ExpiresAfterSeconds != seconds {
		t.Errorf("Expected ExpiresAfterSeconds=%d, got %d", seconds, auth.config.ExpiresAfterSeconds)
	}

	// Test zero value (should not update)
	auth.WithExpiresAfterSeconds(0)
	if auth.config.ExpiresAfterSeconds != seconds {
		t.Errorf("ExpiresAfterSeconds should not change when setting zero value, expected %d, got %d", seconds, auth.config.ExpiresAfterSeconds)
	}

	// Test negative value (should not update)
	auth.WithExpiresAfterSeconds(-100)
	if auth.config.ExpiresAfterSeconds != seconds {
		t.Errorf("ExpiresAfterSeconds should not change when setting negative value, expected %d, got %d", seconds, auth.config.ExpiresAfterSeconds)
	}
}

func TestAuthenticator_WithSkipPaths(t *testing.T) {
	auth := &Authenticator{
		config: &Config{
			SkipAbsolutePaths: make(map[string]struct{}),
			SkipWildcardPaths: make([]string, 0),
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
	if _, exists := auth.config.SkipAbsolutePaths["/api/health"]; !exists {
		t.Error("Expected /api/health in SkipAbsolutePaths")
	}
	if _, exists := auth.config.SkipAbsolutePaths["/docs"]; !exists {
		t.Error("Expected /docs in SkipAbsolutePaths")
	}

	// Check wildcard paths
	if len(auth.config.SkipWildcardPaths) != 2 {
		t.Errorf("Expected 2 wildcard paths, got %d", len(auth.config.SkipWildcardPaths))
	}

	expectedWildcards := []string{"/static", "/public"}
	for _, expected := range expectedWildcards {
		found := false
		for _, actual := range auth.config.SkipWildcardPaths {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected %s in SkipWildcardPaths", expected)
		}
	}

	// Verify empty paths are skipped
	if len(auth.config.SkipAbsolutePaths) != 2 {
		t.Errorf("Expected 2 absolute paths, got %d", len(auth.config.SkipAbsolutePaths))
	}
}

func TestAuthenticator_Sync(t *testing.T) {
	auth := &Authenticator{
		config: &Config{
			SkipAbsolutePaths: make(map[string]struct{}),
			SkipWildcardPaths: []string{"/old"},
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
		SkipAbsolutePaths: map[string]struct{}{
			"/api/health": {},
			"/docs":       {},
		},
		SkipWildcardPaths: []string{"/static", "/public"},
		JWTStandardClaimsConfig: &JWTStandardClaimsConfig{
			Validation:          0,
			NotBeforeSeconds:    300,
			ExpiresAfterSeconds: 400,
			ExceptedIssuer:      "new-issuer",
			ExceptedAudience:    "new-audience",
		},
	}

	auth.SyncConfig(config)

	// Test path synchronization
	if len(auth.config.SkipAbsolutePaths) != 2 {
		t.Errorf("Expected 2 absolute paths after sync, got %d", len(auth.config.SkipAbsolutePaths))
	}
	if _, exists := auth.config.SkipAbsolutePaths["/api/health"]; !exists {
		t.Error("Expected /api/health in SkipAbsolutePaths after sync")
	}
	if _, exists := auth.config.SkipAbsolutePaths["/docs"]; !exists {
		t.Error("Expected /docs in SkipAbsolutePaths after sync")
	}

	if len(auth.config.SkipWildcardPaths) != 2 {
		t.Errorf("Expected 2 wildcard paths after sync, got %d", len(auth.config.SkipWildcardPaths))
	}
	if auth.config.SkipWildcardPaths[0] != "/static" || auth.config.SkipWildcardPaths[1] != "/public" {
		t.Errorf("Unexpected wildcard paths after sync: %v", auth.config.SkipWildcardPaths)
	}

	// Test JWT config synchronization
	if auth.config.Validation != 0 {
		t.Errorf("Expected Validation=0 after sync, got %d", auth.config.Validation)
	}
	if auth.config.NotBeforeSeconds != 300 {
		t.Errorf("Expected NotBeforeSeconds=300 after sync, got %d", auth.config.NotBeforeSeconds)
	}
	if auth.config.ExpiresAfterSeconds != 400 {
		t.Errorf("Expected ExpiresAfterSeconds=400 after sync, got %d", auth.config.ExpiresAfterSeconds)
	}
	if auth.config.ExceptedIssuer != "new-issuer" {
		t.Errorf("Expected ExceptedIssuer=new-issuer after sync, got %s", auth.config.ExceptedIssuer)
	}
	if auth.config.ExceptedAudience != "new-audience" {
		t.Errorf("Expected ExceptedAudience=new-audience after sync, got %s", auth.config.ExceptedAudience)
	}
}

func TestAuthenticator_Sync_WithNilJWTConfig(t *testing.T) {
	auth := &Authenticator{
		config: &Config{
			JWTStandardClaimsConfig: &JWTStandardClaimsConfig{
				Validation: 1,
			},
		},
	}

	config := &Config{
		SkipAbsolutePaths: make(map[string]struct{}),
		SkipWildcardPaths: []string{},
		// JWTStandardClaimsConfig is nil
	}

	auth.SyncConfig(config)

	if auth.config.JWTStandardClaimsConfig == nil {
		t.Error("JWTStandardClaimsConfig should not be nil after sync with nil config")
	}
}
