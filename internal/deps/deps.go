package deps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dj/fetch-track-cli/internal/cache"
)

// Dependency represents a required binary tool and its minimum version.
type Dependency struct {
	Name       string
	MinVersion string
	InstallURL string
}

// RequiredDependencies defines all external binary dependencies and minimum versions required by fetch-track.
var RequiredDependencies = []Dependency{
	{
		Name:       "yt-dlp",
		MinVersion: "2024.08.01",
		InstallURL: "https://github.com/yt-dlp/yt-dlp#installation",
	},
	{
		Name:       "ffmpeg",
		MinVersion: "4.4",
		InstallURL: "https://ffmpeg.org/download.html",
	},
	{
		Name:       "ffprobe",
		MinVersion: "4.4",
		InstallURL: "https://ffmpeg.org/download.html",
	},
}

// CommandRunner abstracts execution of external version commands for testing.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultRunner uses os/exec to execute system commands.
func DefaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// DependencyReport describes the verification status of a single dependency.
type DependencyReport struct {
	Name            string `json:"name"`
	MinVersion      string `json:"minVersion"`
	DetectedVersion string `json:"detectedVersion"`
	Installed       bool   `json:"installed"`
	Satisfied       bool   `json:"satisfied"`
	Error           string `json:"error,omitempty"`
}

// CheckDependencies verifies all required external binary tools are installed and meet minimum version requirements.
func CheckDependencies(ctx context.Context) error {
	return CheckDependenciesWithRunner(ctx, DefaultRunner, RequiredDependencies...)
}

// CheckDependenciesWithRunner checks dependencies using a provided CommandRunner.
func CheckDependenciesWithRunner(ctx context.Context, runner CommandRunner, deps ...Dependency) error {
	_, err := VerifyDependenciesWithRunner(ctx, runner, nil, deps...)
	return err
}

// IsAgentMode checks if AGENT=1 or AGENT=true environment variable is set.
func IsAgentMode() bool {
	val := strings.TrimSpace(os.Getenv("AGENT"))
	return val == "1" || strings.ToLower(val) == "true"
}

// VerifyDependencies inspects all required external binary tools and returns individual status reports and an error if any fail.
func VerifyDependencies(ctx context.Context, cacheInst ...*cache.Cache) ([]DependencyReport, error) {
	var c *cache.Cache
	if len(cacheInst) > 0 {
		c = cacheInst[0]
	} else {
		c, _ = cache.New(true)
	}
	return VerifyDependenciesWithRunner(ctx, DefaultRunner, c, RequiredDependencies...)
}

// VerifyDependenciesWithRunner inspects dependencies using a provided CommandRunner.
func VerifyDependenciesWithRunner(ctx context.Context, runner CommandRunner, c *cache.Cache, deps ...Dependency) ([]DependencyReport, error) {
	var reports []DependencyReport
	var firstErr error

	for _, dep := range deps {
		report := DependencyReport{
			Name:       dep.Name,
			MinVersion: dep.MinVersion,
		}

		var out []byte
		var err error

		var cachedStr string
		if c != nil && c.Get("deps", dep.Name, &cachedStr) && cachedStr != "" {
			out = []byte(cachedStr)
		} else {
			var versionArgs []string
			if dep.Name == "yt-dlp" {
				versionArgs = []string{"--version"}
			} else {
				versionArgs = []string{"-version"}
			}

			out, err = runner(ctx, dep.Name, versionArgs...)
			if err == nil && len(out) > 0 && c != nil {
				_ = c.Put("deps", dep.Name, string(out), 10*time.Minute)
			}
		}

		isAgent := IsAgentMode()

		if err != nil {
			if isNotFound(err) {
				report.Installed = false
				report.Satisfied = false
				if isAgent {
					report.Error = fmt.Sprintf("%s is missing in $PATH. Ask user for confirmation to install %s version %s or newer.", dep.Name, dep.Name, dep.MinVersion)
				} else {
					report.Error = fmt.Sprintf("%s is missing in $PATH, install version %s or newer", dep.Name, dep.MinVersion)
				}
			} else {
				report.Installed = false
				report.Satisfied = false
				report.Error = fmt.Sprintf("failed to check %s version: %v", dep.Name, err)
			}
		} else {
			report.Installed = true
			versionStr := ParseVersionOutput(dep.Name, string(out))
			report.DetectedVersion = versionStr

			if versionStr == "" {
				report.Satisfied = false
				report.Error = fmt.Sprintf("could not parse %s version output", dep.Name)
			} else if compErr := CompareVersions(versionStr, dep.MinVersion); compErr != nil {
				report.Satisfied = false
				if isAgent {
					report.Error = fmt.Sprintf("%s version %s in $PATH is outdated. Ask user for confirmation to update %s to version %s or newer.", dep.Name, versionStr, dep.Name, dep.MinVersion)
				} else {
					report.Error = fmt.Sprintf("%s in $PATH must be version %s or newer", dep.Name, dep.MinVersion)
				}
			} else {
				report.Satisfied = true
			}
		}

		reports = append(reports, report)
		if !report.Satisfied && firstErr == nil {
			firstErr = errors.New(report.Error)
		}
	}

	return reports, firstErr
}

// CheckDependency checks a single dependency for existence and minimum version requirement.
func CheckDependency(ctx context.Context, runner CommandRunner, dep Dependency) error {
	_, err := VerifyDependenciesWithRunner(ctx, runner, nil, dep)
	return err
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if execErr, ok := err.(*exec.Error); ok && execErr.Err == exec.ErrNotFound {
		return true
	}
	errStr := err.Error()
	return strings.Contains(errStr, "executable file not found") || strings.Contains(errStr, "not found")
}

// ParseVersionOutput extracts the version string from tool stdout output.
func ParseVersionOutput(depName, output string) string {
	line := strings.TrimSpace(output)
	if idx := strings.Index(line, "\n"); idx != -1 {
		line = strings.TrimSpace(line[:idx])
	}

	switch depName {
	case "yt-dlp":
		re := regexp.MustCompile(`\b(\d{4}\.\d{2}\.\d{2}(?:\.\d+)?)\b`)
		match := re.FindStringSubmatch(line)
		if len(match) > 1 {
			return match[1]
		}
		return line
	case "ffmpeg", "ffprobe":
		re := regexp.MustCompile(`(?:ffmpeg|ffprobe)\s+version\s+(?:n)?([^\s]+)`)
		match := re.FindStringSubmatch(line)
		if len(match) > 1 {
			return match[1]
		}
	}
	return line
}

// CompareVersions checks if actual version meets or exceeds minVersion.
func CompareVersions(actualStr, minStr string) error {
	if strings.HasPrefix(actualStr, "N-") || strings.HasPrefix(actualStr, "git-") || strings.HasPrefix(actualStr, "DEV-") {
		return nil
	}

	actualParts := parseVersionParts(actualStr)
	minParts := parseVersionParts(minStr)

	if len(actualParts) == 0 {
		return fmt.Errorf("invalid version string %q", actualStr)
	}

	for i := 0; i < len(minParts); i++ {
		actVal := 0
		if i < len(actualParts) {
			actVal = actualParts[i]
		}
		minVal := minParts[i]

		if actVal > minVal {
			return nil
		}
		if actVal < minVal {
			return fmt.Errorf("version below minimum requirement")
		}
	}

	return nil
}

func parseVersionParts(v string) []int {
	if idx := strings.IndexAny(v, "-+"); idx != -1 {
		v = v[:idx]
	}

	rawParts := strings.Split(v, ".")
	var parts []int
	for _, p := range rawParts {
		digits := ""
		for _, ch := range p {
			if ch >= '0' && ch <= '9' {
				digits += string(ch)
			} else {
				break
			}
		}
		if digits == "" {
			break
		}
		val, err := strconv.Atoi(digits)
		if err != nil {
			break
		}
		parts = append(parts, val)
	}
	return parts
}
