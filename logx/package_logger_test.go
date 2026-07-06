package logx_test

import (
	"strings"
	"testing"

	logx "github.com/sh-zou/go-logx/logx"
)

func TestPackageUsesCallingPackageName(t *testing.T) {
	name := logx.Package().Named()
	if !strings.HasSuffix(name, "logx_test") {
		t.Fatalf("Package().Named() = %q, want suffix logx_test", name)
	}
}

func TestPackageSkipUsesWrappedCallingPackageName(t *testing.T) {
	name := packageLoggerFromAdapter().Named()
	if !strings.HasSuffix(name, "logx_test") {
		t.Fatalf("PackageSkip().Named() = %q, want suffix logx_test", name)
	}
}

func packageLoggerFromAdapter() logx.ModuleLogger {
	return logx.PackageSkip(1)
}
