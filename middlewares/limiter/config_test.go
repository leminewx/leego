package limiter

import (
	"testing"
	"time"
)

func TestNewDefaultConfig(t *testing.T) {
	config := NewDefaultConfig()

	if config.WaitTimeout != 0 {
		t.Fatalf("WaitTimeout = %v, want 0", config.WaitTimeout)
	}

	if config.PollInterval != _DEFAULT_POLL_INTERVAL {
		t.Fatalf("PollInterval = %v, want %v", config.PollInterval, _DEFAULT_POLL_INTERVAL)
	}
}

func TestConfigNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input Config
		want  Config
	}{
		{
			name:  "零值回退默认轮询间隔但保留零等待",
			input: Config{},
			want:  Config{WaitTimeout: 0, PollInterval: _DEFAULT_POLL_INTERVAL},
		},
		{
			name:  "负数等待按不等待处理",
			input: Config{WaitTimeout: -time.Second, PollInterval: time.Millisecond},
			want:  Config{WaitTimeout: 0, PollInterval: time.Millisecond},
		},
		{
			name:  "负数轮询间隔回退默认值",
			input: Config{WaitTimeout: time.Second, PollInterval: -1},
			want:  Config{WaitTimeout: time.Second, PollInterval: _DEFAULT_POLL_INTERVAL},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.input.normalize()
			if got != test.want {
				t.Fatalf("normalize() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestResolveConfig(t *testing.T) {
	defaults := resolveConfig()
	if defaults != NewDefaultConfig() {
		t.Fatalf("resolveConfig() = %+v, want default", defaults)
	}

	custom := Config{WaitTimeout: 2 * time.Second, PollInterval: 10 * time.Millisecond}
	if got := resolveConfig(custom); got != custom {
		t.Fatalf("resolveConfig(custom) = %+v, want %+v", got, custom)
	}

	// 多余参数被忽略，取第一个（避免优先级歧义）。
	if got := resolveConfig(custom, Config{WaitTimeout: time.Minute}); got != custom {
		t.Fatalf("resolveConfig(first, second) = %+v, want first", got)
	}

	// 非法值在解析阶段被归一化。
	if got := resolveConfig(Config{WaitTimeout: -1}); got.WaitTimeout != 0 {
		t.Fatalf("resolveConfig(invalid).WaitTimeout = %v, want 0", got.WaitTimeout)
	}
}

func TestLimiterUsesConfig(t *testing.T) {
	custom := Config{WaitTimeout: time.Second, PollInterval: 7 * time.Millisecond}

	fixed := NewFixedWindowLimiter(1, time.Second, custom)
	if fixed.options != custom {
		t.Fatalf("fixed.options = %+v, want %+v", fixed.options, custom)
	}

	sliding := NewSlidingWindowLimiter(1, 2, time.Second, custom)
	if sliding.options != custom {
		t.Fatalf("sliding.options = %+v, want %+v", sliding.options, custom)
	}

	token := NewTokenBucketLimiter(1, 1, custom)
	if token.options != custom {
		t.Fatalf("token.options = %+v, want %+v", token.options, custom)
	}

	// 未传配置时使用默认值。
	if NewFixedWindowLimiter(1, time.Second).options != NewDefaultConfig() {
		t.Fatal("未传配置时应使用默认值")
	}
}

func TestConfigClone(t *testing.T) {
	original := Config{WaitTimeout: time.Second, PollInterval: time.Millisecond}
	if clone := original.Clone(); clone != original {
		t.Fatalf("Clone() = %+v, want %+v", clone, original)
	}
}
