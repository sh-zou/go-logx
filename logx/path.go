package logx

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func validateInitPaths(appName string, cfg Config) error {
	if err := validatePathSegment("appName", appName, true); err != nil {
		return err
	}
	if err := validatePathSegment("fileName", cfg.FileName, true); err != nil {
		return err
	}
	seenSinks := make(map[string]struct{}, len(cfg.Sinks))
	for rawName, sinkCfg := range cfg.Sinks {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if _, exists := seenSinks[name]; exists {
			return fmt.Errorf("duplicate sink name after trimming: %q", name)
		}
		seenSinks[name] = struct{}{}
		if _, err := cleanRelativePath(sinkCfg.Dir, true); err != nil {
			return fmt.Errorf("sink %q dir: %w", name, err)
		}
		fileName := strings.TrimSpace(sinkCfg.FileName)
		if fileName == "" {
			fileName = name + ".log"
		}
		if err := validatePathSegment("fileName", fileName, false); err != nil {
			return fmt.Errorf("sink %q: %w", name, err)
		}
	}
	return nil
}

func validatePathSegment(label, value string, allowEmpty bool) error {
	value = strings.TrimSpace(value)
	if value == "" && allowEmpty {
		return nil
	}
	cleaned, err := cleanRelativePath(value, false)
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", label, value, err)
	}
	if filepath.Base(cleaned) != cleaned {
		return fmt.Errorf("invalid %s %q: path separators are not allowed", label, value)
	}
	return nil
}

func cleanRelativePath(value string, allowEmpty bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if allowEmpty {
			return "", nil
		}
		return "", fmt.Errorf("path is empty")
	}
	if strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("path contains a NUL byte")
	}
	portable := strings.ReplaceAll(value, "\\", "/")
	if path.IsAbs(portable) || filepath.IsAbs(value) || filepath.VolumeName(value) != "" {
		return "", fmt.Errorf("path must be relative")
	}
	cleaned := path.Clean(portable)
	if cleaned == "." {
		if allowEmpty {
			return "", nil
		}
		return "", fmt.Errorf("path is empty")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path escapes the log directory")
	}
	native := filepath.FromSlash(cleaned)
	if !filepath.IsLocal(native) {
		return "", fmt.Errorf("path is not local")
	}
	return native, nil
}

func canonicalLogPath(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	if runtime.GOOS == "windows" {
		absolute = strings.ToLower(absolute)
	}
	return absolute, nil
}

func canonicalLogPathWithin(baseDir, targetPath string) (string, error) {
	resolvedBase, err := resolveWithExistingAncestor(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve log base directory: %w", err)
	}
	resolvedTarget, err := resolveWithExistingAncestor(targetPath)
	if err != nil {
		return "", fmt.Errorf("resolve log target path: %w", err)
	}
	relative, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil {
		return "", fmt.Errorf("compare log paths: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("resolved path %q escapes log directory %q", resolvedTarget, resolvedBase)
	}
	return canonicalLogPath(resolvedTarget)
}

func resolveWithExistingAncestor(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	var missing []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
