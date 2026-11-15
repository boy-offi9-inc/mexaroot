package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const Name = "mexarootd"

var (
	ErrNoPrefix           = errors.New("PREFIX is not set; run this inside Termux")
	ErrRelativeExecutable = errors.New("executable path must be absolute")
	ErrServiceEnabled     = errors.New("service is enabled; disable it first with: sv-disable mexarootd")
)

type Spec struct {
	Prefix     string
	Executable string
	Args       []string
}

func Dir(prefix string) string {
	return filepath.Join(prefix, "var", "service", Name)
}

func LogDir(prefix string) string {
	return filepath.Join(prefix, "var", "log", "sv", Name)
}

func Install(spec Spec) (string, error) {
	if spec.Prefix == "" {
		return "", ErrNoPrefix
	}
	if !filepath.IsAbs(spec.Executable) {
		return "", fmt.Errorf("%w: %q", ErrRelativeExecutable, spec.Executable)
	}

	dir := Dir(spec.Prefix)
	_, statErr := os.Stat(dir)
	isNew := errors.Is(statErr, os.ErrNotExist)

	logDir := LogDir(spec.Prefix)
	for _, path := range []string{filepath.Join(dir, "log"), logDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return "", fmt.Errorf("create %s: %w", path, err)
		}
	}

	if err := writeScript(filepath.Join(dir, "run"), runScript(spec)); err != nil {
		return "", err
	}
	if err := writeScript(filepath.Join(dir, "log", "run"), logScript(spec.Prefix, logDir)); err != nil {
		return "", err
	}

	if isNew {
		if err := os.WriteFile(filepath.Join(dir, "down"), nil, 0o644); err != nil {
			return "", fmt.Errorf("create down file: %w", err)
		}
	}
	return dir, nil
}

func Uninstall(prefix string) error {
	if prefix == "" {
		return ErrNoPrefix
	}
	dir := Dir(prefix)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "down")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrServiceEnabled
		}
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	return nil
}

func runScript(spec Spec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#!%s\n", shellPath(spec.Prefix))
	b.WriteString("exec 2>&1\n")
	b.WriteString("exec " + shellQuote(spec.Executable))
	for _, arg := range spec.Args {
		b.WriteString(" " + shellQuote(arg))
	}
	b.WriteString("\n")
	return b.String()
}

func logScript(prefix, logDir string) string {
	svlogd := filepath.Join(prefix, "bin", "svlogd")
	return fmt.Sprintf("#!%s\nexec %s -tt %s\n", shellPath(prefix), shellQuote(svlogd), shellQuote(logDir))
}

func shellPath(prefix string) string {
	return filepath.Join(prefix, "bin", "sh")
}

func shellQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

func writeScript(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("make %s executable: %w", path, err)
	}
	return nil
}
