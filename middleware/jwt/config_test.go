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

	if config.StandardClaimsConfig == nil {
		t.Fatal("StandardClaimsConfig should be initialized")
	}

	if config.Validation != 1 {
		t.Errorf("Expected Validation=1, got %d", config.Validation)
	}

	if config.ExpiresAfterSeconds != 24*3600 {
		t.Errorf("Expected ExpiresAfterSeconds=86400, got %d", config.ExpiresAfterSeconds)
	}
}

func TestManager_SetScope(t *testing.T) {
	auth := &Manager{config: &Config{StandardClaimsConfig: &StandardClaimsConfig{}}}
	scope := "test-scope"

	auth.SetScope(scope)
	if auth.config.Scope != scope {
		t.Errorf("Expected scope %s, got %s", scope, auth.config.Scope)
	}
}

func TestManager_SetExceptedIssuer(t *testing.T) {
	auth := &Manager{config: &Config{StandardClaimsConfig: &StandardClaimsConfig{}}}
	issuer := "test-issuer"

	auth.SetExceptedIssuer(issuer)

	if auth.config.ExceptedIssuer != strings.TrimSpace(issuer) {
		t.Errorf("Expected issuer %s, got %s", issuer, auth.config.ExceptedIssuer)
	}

	// Test with spaces
	auth.SetExceptedIssuer("  issuer-with-spaces  ")
	if auth.config.ExceptedIssuer != "issuer-with-spaces" {
		t.Errorf("Expected trimmed issuer, got %s", auth.config.ExceptedIssuer)
	}
}

func TestManager_SetExceptedAudience(t *testing.T) {
	auth := &Manager{config: &Config{StandardClaimsConfig: &StandardClaimsConfig{}}}
	audience := "test-audience"
	auth.SetExceptedAudience(audience)

	if auth.config.ExceptedAudience != strings.TrimSpace(audience) {
		t.Errorf("Expected audience %s, got %s", audience, auth.config.ExceptedAudience)
	}
}

func TestManager_SetNotBeforeSeconds(t *testing.T) {
	auth := &Manager{config: &Config{StandardClaimsConfig: &StandardClaimsConfig{}}}

	// Test positive value
	seconds := int64(300)
	auth.SetNotBeforeSeconds(seconds)

	if auth.config.NotBeforeSeconds != seconds {
		t.Errorf("Expected NotBeforeSeconds=%d, got %d", seconds, auth.config.NotBeforeSeconds)
	}

	// Test zero value (should not update)
	auth.SetNotBeforeSeconds(0)
	if auth.config.NotBeforeSeconds != seconds {
		t.Errorf("NotBeforeSeconds should not change when setting zero value, expected %d, got %d", seconds, auth.config.NotBeforeSeconds)
	}

	// Test negative value (should not update)
	auth.SetNotBeforeSeconds(-100)
	if auth.config.NotBeforeSeconds != seconds {
		t.Errorf("NotBeforeSeconds should not change when setting negative value, expected %d, got %d", seconds, auth.config.NotBeforeSeconds)
	}
}

func TestManager_SetExpiresAfterSeconds(t *testing.T) {
	auth := &Manager{config: &Config{StandardClaimsConfig: &StandardClaimsConfig{}}}

	// Test positive value
	seconds := int64(3600)
	auth.SetExpiresAfterSeconds(seconds)

	if auth.config.ExpiresAfterSeconds != seconds {
		t.Errorf("Expected ExpiresAfterSeconds=%d, got %d", seconds, auth.config.ExpiresAfterSeconds)
	}

	// Test zero value (should not update)
	auth.SetExpiresAfterSeconds(0)
	if auth.config.ExpiresAfterSeconds != seconds {
		t.Errorf("ExpiresAfterSeconds should not change when setting zero value, expected %d, got %d", seconds, auth.config.ExpiresAfterSeconds)
	}

	// Test negative value (should not update)
	auth.SetExpiresAfterSeconds(-100)
	if auth.config.ExpiresAfterSeconds != seconds {
		t.Errorf("ExpiresAfterSeconds should not change when setting negative value, expected %d, got %d", seconds, auth.config.ExpiresAfterSeconds)
	}
}

func TestManager_SetSkipPaths(t *testing.T) {
	auth := &Manager{
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

	auth.SetSkipPaths(paths...)

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

func TestManager_Sync(t *testing.T) {
	auth := &Manager{
		config: &Config{
			SkipAbsolutePaths: make(map[string]struct{}),
			SkipWildcardPaths: []string{"/old"},
			StandardClaimsConfig: &StandardClaimsConfig{
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
		StandardClaimsConfig: &StandardClaimsConfig{
			Validation:          0,
			NotBeforeSeconds:    300,
			ExpiresAfterSeconds: 400,
			ExceptedIssuer:      "new-issuer",
			ExceptedAudience:    "new-audience",
		},
	}

	if err := auth.SyncConfig(config); err != nil {
		t.Error(err)
	}

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

func TestManager_Sync_SetNilJWTConfig(t *testing.T) {
	auth := &Manager{
		config: &Config{
			StandardClaimsConfig: &StandardClaimsConfig{
				Validation: 1,
			},
		},
	}

	config := &Config{
		SkipAbsolutePaths: make(map[string]struct{}),
		SkipWildcardPaths: []string{},
		// StandardClaimsConfig is nil
	}

	if err := auth.SyncConfig(config); err != nil {
		t.Error(err)
	}

	if auth.config.StandardClaimsConfig == nil {
		t.Error("StandardClaimsConfig should not be nil after sync with nil config")
	}
}
