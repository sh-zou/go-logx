package logx

import (
	"path"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

var (
	packageNameCache sync.Map
	modulePathPrefix = detectModulePathPrefix()
)

// Package 返回按调用方包路径命名的日志包装器。
// 例如 example.com/project/internal/app/bootstrap 会映射为 internal.app.bootstrap。
//
//go:noinline
func Package() ModuleLogger {
	return Module(callerModuleName(2))
}

// PackageSkip 返回按调用方包路径命名的日志包装器，并额外跳过 skip 层封装。
// 适用于项目内保留 logger.Infof 这类适配层时，让日志名称仍指向真实业务调用包。
//
//go:noinline
func PackageSkip(skip int) ModuleLogger {
	if skip < 0 {
		skip = 0
	}
	return Module(callerModuleName(skip + 2))
}

//go:noinline
func callerModuleName(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return ""
	}
	cacheKey := pc
	if cached, ok := packageNameCache.Load(cacheKey); ok {
		name, _ := cached.(string)
		return name
	}
	name := normalizeModuleName(functionImportPath(pc))
	packageNameCache.Store(cacheKey, name)
	return name
}

func functionImportPath(pc uintptr) string {
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return ""
	}
	return trimFunctionName(fn.Name())
}

func normalizeModuleName(importPath string) string {
	importPath = strings.TrimSpace(importPath)
	if modulePathPrefix != "" {
		importPath = strings.TrimPrefix(importPath, modulePathPrefix)
	}
	importPath = strings.Trim(importPath, "/")
	if importPath == "" {
		return ""
	}
	return strings.ReplaceAll(importPath, "/", ".")
}

func detectModulePathPrefix() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return ""
	}
	modulePath := strings.TrimSpace(info.Main.Path)
	if modulePath == "" {
		modulePath = strings.TrimSpace(info.Path)
	}
	modulePath = path.Clean(strings.ReplaceAll(modulePath, "\\", "/"))
	if modulePath == "." || modulePath == "/" {
		return ""
	}
	modulePath = strings.Trim(modulePath, "/")
	if modulePath == "" {
		return ""
	}
	return modulePath + "/"
}

func trimFunctionName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if index := strings.LastIndex(name, "/"); index >= 0 {
		tail := name[index+1:]
		if dot := strings.Index(tail, "."); dot >= 0 {
			return strings.TrimSpace(name[:index+1+dot])
		}
		return strings.TrimSpace(name)
	}
	if dot := strings.Index(name, "."); dot >= 0 {
		return strings.TrimSpace(name[:dot])
	}
	return strings.TrimSpace(name)
}
