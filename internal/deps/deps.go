package deps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/alexgorbatchev/fetch-track-cli/internal/cache"
	"github.com/alexgorbatchev/godeps/v2"
)

// AppName is the application name used for managed directories and XDG storage.
const AppName = "fetch-track"

// Dependency alias for godeps.Dependency.
type Dependency = godeps.Dependency

// DependencyReport alias for godeps.DependencyReport.
type DependencyReport = godeps.DependencyReport

// CommandRunner alias for godeps.CommandRunner.
type CommandRunner = godeps.CommandRunner

// DefaultRunner is RunCommand.
var DefaultRunner CommandRunner = RunCommand

var (
	defaultRunnerMu sync.Mutex
	currentRunner   CommandRunner = RunCommand
)

// SetDefaultRunner overrides the default command runner and returns a restore cleanup function.
func SetDefaultRunner(runner CommandRunner) func() {
	defaultRunnerMu.Lock()
	orig := currentRunner
	currentRunner = runner
	defaultRunnerMu.Unlock()

	return func() {
		defaultRunnerMu.Lock()
		currentRunner = orig
		defaultRunnerMu.Unlock()
	}
}

// GetRunner returns the active CommandRunner.
func GetRunner() CommandRunner {
	defaultRunnerMu.Lock()
	defer defaultRunnerMu.Unlock()
	if currentRunner == nil {
		return RunCommand
	}
	return currentRunner
}

// RequiredDependencies defines external dependencies required by fetch-track.
var RequiredDependencies = []Dependency{
	{
		Name:         "yt-dlp",
		MinVersion:   "2024.08.01",
		InstallURL:   "https://github.com/yt-dlp/yt-dlp#installation",
		VersionArgs:  []string{"--version"},
		ParseVersion: ParseVersionOutput,
		Installer:    godeps.YtDlp(),
	},
	{
		Name:         "ffmpeg",
		MinVersion:   "4.4",
		InstallURL:   "https://ffmpeg.org/download.html",
		VersionArgs:  []string{"-version"},
		ParseVersion: ParseVersionOutput,
		Installer:    godeps.SystemPackageManager("ffmpeg"),
	},
	{
		Name:         "ffprobe",
		MinVersion:   "4.4",
		InstallURL:   "https://ffmpeg.org/download.html",
		VersionArgs:  []string{"-version"},
		ParseVersion: ParseVersionOutput,
		Installer:    godeps.SystemPackageManager("ffmpeg"),
	},
	{
		Name:         "tag-track",
		MinVersion:   "1.0.0",
		InstallURL:   "https://github.com/alexgorbatchev/tag-track-cli#installation",
		VersionArgs:  []string{"--version"},
		ParseVersion: ParseVersionOutput,
		Installer:    godeps.GitHubReleaseGoBinary("alexgorbatchev", "tag-track-cli", "tag-track"),
	},
}

// cacheAdapter adapts internal/cache.Cache to godeps.Cache.
type cacheAdapter struct {
	cache *cache.Cache
}

func (a *cacheAdapter) Get(key string, target any) bool {
	if a == nil || a.cache == nil {
		return false
	}
	cleanKey := strings.TrimPrefix(key, "godeps_")
	cleanKey = strings.TrimSuffix(cleanKey, ".json")
	return a.cache.Get("deps", cleanKey, target) || a.cache.Get("deps", key, target)
}

func (a *cacheAdapter) Put(key string, val any) error {
	if a == nil || a.cache == nil {
		return nil
	}
	cleanKey := strings.TrimPrefix(key, "godeps_")
	cleanKey = strings.TrimSuffix(cleanKey, ".json")
	return a.cache.Put("deps", cleanKey, val, 10*time.Minute)
}

func (a *cacheAdapter) Delete(key string) error {
	if a == nil || a.cache == nil {
		return nil
	}
	cleanKey := strings.TrimPrefix(key, "godeps_")
	cleanKey = strings.TrimSuffix(cleanKey, ".json")
	_ = a.cache.Delete("deps", key)
	return a.cache.Delete("deps", cleanKey)
}

// NewManager creates a configured godeps.Manager instance for fetch-track.
func NewManager(cacheInst *cache.Cache, runner ...CommandRunner) *godeps.Manager {
	r := GetRunner()
	if len(runner) > 0 && runner[0] != nil {
		r = runner[0]
	}

	var c godeps.Cache
	if cacheInst != nil {
		c = &cacheAdapter{cache: cacheInst}
	}

	return godeps.New(godeps.Config{
		AppName:      AppName,
		Dependencies: RequiredDependencies,
		Runner:       r,
		Cache:        c,
	})
}

// CheckDependencies verifies all required external binary tools are installed and meet minimum version requirements.
func CheckDependencies(ctx context.Context, cacheInst ...*cache.Cache) error {
	var c *cache.Cache
	if len(cacheInst) > 0 {
		c = cacheInst[0]
	}
	_, err := VerifyDependencies(ctx, c)
	return err
}

// VerifyDependencies inspects all required external binary tools and returns individual status reports.
func VerifyDependencies(ctx context.Context, cacheInst ...*cache.Cache) ([]DependencyReport, error) {
	var c *cache.Cache
	if len(cacheInst) > 0 {
		c = cacheInst[0]
	}
	return VerifyDependenciesWithRunner(ctx, GetRunner(), c, RequiredDependencies...)
}

// VerifyDependenciesWithRunner inspects dependencies using a provided CommandRunner.
func VerifyDependenciesWithRunner(ctx context.Context, runner CommandRunner, c *cache.Cache, deps ...Dependency) ([]DependencyReport, error) {
	if len(deps) == 0 {
		deps = RequiredDependencies
	}
	var ca godeps.Cache
	if c != nil {
		ca = &cacheAdapter{cache: c}
	}

	safeRunner := runner
	if runner != nil {
		safeRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			out, err := runner(ctx, name, args...)
			if err != nil {
				cleanErr := godeps.SanitizeStderr(err.Error())
				if cleanErr != "" && cleanErr != err.Error() {
					return out, &sanitizedError{message: cleanErr, cause: err}
				}
				return out, err
			}
			return out, nil
		}
	}

	mgr := godeps.New(godeps.Config{
		AppName:      AppName,
		Dependencies: deps,
		Runner:       safeRunner,
		Cache:        ca,
	})
	return mgr.Verify(ctx)
}

// sanitizedError shows a tool failure without its stack trace while keeping the
// original error, so godeps still recognizes a missing executable.
type sanitizedError struct {
	message string
	cause   error
}

func (e *sanitizedError) Error() string { return e.message }
func (e *sanitizedError) Unwrap() error { return e.cause }

// CheckDependenciesWithRunner checks dependencies using a provided CommandRunner.
func CheckDependenciesWithRunner(ctx context.Context, runner CommandRunner, c *cache.Cache, deps ...Dependency) error {
	_, err := VerifyDependenciesWithRunner(ctx, runner, c, deps...)
	return err
}

// CheckDependency checks a single dependency for existence and minimum version requirement.
func CheckDependency(ctx context.Context, runner CommandRunner, dep Dependency) error {
	_, err := VerifyDependenciesWithRunner(ctx, runner, nil, dep)
	return err
}

// InstallDependency installs a single dependency by name into managed directory.
func InstallDependency(ctx context.Context, depName string) error {
	c, _ := cache.New(true)
	mgr := NewManager(c)
	return mgr.Install(ctx, depName)
}

// UpdateDependency updates a single dependency by name.
func UpdateDependency(ctx context.Context, depName string) error {
	c, _ := cache.New(true)
	mgr := NewManager(c)
	return mgr.Update(ctx, depName)
}

// InstallMissingDependencies installs all unsatisfied dependencies and
// replaces a managed one-file yt-dlp with the unpacked build.
func InstallMissingDependencies(ctx context.Context) ([]string, error) {
	c, _ := cache.New(true)
	mgr := NewManager(c)
	installed, err := mgr.InstallUnsatisfied(ctx)
	if err != nil || slices.Contains(installed, ytDlpName) || !HasOneFileYtDlp() {
		return installed, err
	}
	if err := mgr.Install(ctx, ytDlpName); err != nil {
		return installed, fmt.Errorf("replacing the one-file %s build: %w", ytDlpName, err)
	}
	return append(installed, ytDlpName), nil
}

// ytDlpName is the yt-dlp dependency's name.
const ytDlpName = "yt-dlp"

// HasOneFileYtDlp reports whether the managed bin directory holds a yt-dlp
// executable file rather than the unpacked build's package directory. The
// one-file build unpacks about 70 MB into the system temporary directory on
// every run, so installation replaces it even when its version is current.
func HasOneFileYtDlp() bool {
	binDir, err := godeps.GetManagedBinDir(AppName)
	if err != nil {
		return false
	}
	for _, name := range []string{ytDlpName, ytDlpName + ".exe"} {
		if info, err := os.Stat(filepath.Join(binDir, name)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// UpdateAllDependencies updates all declared dependencies.
func UpdateAllDependencies(ctx context.Context) ([]string, error) {
	c, _ := cache.New(true)
	mgr := NewManager(c)
	return mgr.UpdateAll(ctx)
}

// managedPlacement remembers the PATH that InitManagedPath last produced and
// the managed directory it placed.
var managedPlacement struct {
	sync.Mutex
	binDir string
	path   string
}

// InitManagedPath puts the managed directories on the process PATH so each
// dependency resolves to the copy it should use: a copy elsewhere on PATH that
// meets the minimum version wins, and the managed copy is moved ahead only when
// that copy is missing or too old. Deciding that runs the version commands of
// dependencies that have both copies, so it needs ctx. It does nothing while
// PATH and the managed directory are still what its last placement produced.
func InitManagedPath(ctx context.Context) error {
	binDir, err := godeps.GetManagedBinDir(AppName)
	if err != nil {
		return err
	}
	managedPlacement.Lock()
	defer managedPlacement.Unlock()
	if managedPlacement.binDir == binDir && managedPlacement.path == os.Getenv("PATH") {
		return nil
	}
	if err := NewManager(nil).InitManagedPath(ctx); err != nil {
		return err
	}
	managedPlacement.binDir, managedPlacement.path = binDir, os.Getenv("PATH")
	return nil
}

// RunCommand runs a non-interactive external tool and returns its standard
// output. The tool runs through godeps.CommandContext, so a cancelled or
// timed-out ctx sends SIGTERM to its whole process group and SIGKILL only after
// a grace period, giving tools such as yt-dlp time to remove their temporary
// files. Errors carry the tool's sanitized standard error with the home
// directory folded.
func RunCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := godeps.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, CleanErrorMessage(msg))
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

var upgradeSelfFunc = godeps.UpgradeSelf

// SetUpgradeSelfFunc overrides UpgradeSelf for testing.
func SetUpgradeSelfFunc(fn func(ctx context.Context, owner, repo, currentVersion string) (string, error)) func() {
	orig := upgradeSelfFunc
	upgradeSelfFunc = fn
	return func() {
		upgradeSelfFunc = orig
	}
}

// UpgradeSelf upgrades the running CLI binary to the latest GitHub release.
func UpgradeSelf(ctx context.Context, currentVersion string) (string, error) {
	return upgradeSelfFunc(ctx, "alexgorbatchev", "fetch-track-cli", currentVersion)
}

// UpgradeSelfToPath upgrades the binary at destPath to the latest GitHub release.
func UpgradeSelfToPath(ctx context.Context, currentVersion, destPath string) (string, error) {
	return godeps.UpgradeSelfToPath(ctx, "alexgorbatchev", "fetch-track-cli", currentVersion, destPath)
}

// IsAgentMode checks if AGENT=1 or AGENT=true is set.
func IsAgentMode() bool {
	return godeps.IsAgentMode()
}

// CleanErrorMessage cleans stderr outputs from external tools using godeps.SanitizeStderr.
func CleanErrorMessage(msg string) string {
	return FoldHome(godeps.SanitizeStderr(msg))
}

// CompareVersions compares actual against min version using godeps.CompareVersions.
func CompareVersions(actual, min string) error {
	return godeps.CompareVersions(actual, min)
}

// ParseVersionOutput extracts the version string from tool stdout output.
func ParseVersionOutput(depName, output string) string {
	line := strings.TrimSpace(output)
	if idx := strings.Index(line, "\n"); idx != -1 {
		line = strings.TrimSpace(line[:idx])
	}

	switch strings.ToLower(depName) {
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
