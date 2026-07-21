package logx

import "testing"

func TestCloseClearsLoggerCachesAcrossGenerations(t *testing.T) {
	namedCache.Clear()
	sinkCache.Clear()

	for i := 0; i < 5; i++ {
		if err := Init("api", Config{
			Dir: t.TempDir(),
			Sinks: map[string]SinkConfig{
				"access": {},
			},
		}); err != nil {
			t.Fatalf("Init() error = %v", err)
		}
		Named("main")
		SinkNamed("access", "request")
		Module("module").Logger()
		Close()
	}

	if got := syncMapSize(&namedCache); got != 0 {
		t.Fatalf("named cache size = %d, want 0", got)
	}
	if got := syncMapSize(&sinkCache); got != 0 {
		t.Fatalf("sink cache size = %d, want 0", got)
	}
}

func syncMapSize(cache interface {
	Range(func(key, value any) bool)
}) int {
	size := 0
	cache.Range(func(_, _ any) bool {
		size++
		return true
	})
	return size
}
