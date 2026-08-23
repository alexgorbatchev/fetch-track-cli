package deps

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alexgorbatchev/godeps"
	"github.com/dj/fetch-track-cli/internal/cache"
)

// AppName is the application name used for managed directories and XDG storage.
const AppName = "fetch-track"

// Dependency alias for godeps.Dependency.
type Dependency = godeps.Dependency

// DependencyReport alias for godeps.DependencyReport.
type DependencyReport = godeps.DependencyReport

// CommandRunner alias for godeps.CommandRunner.
type CommandRunner = godeps.CommandRunner

// DefaultRunner uses godeps.DefaultRunner.
var DefaultRunner = godeps.DefaultRunner

var (
	defaultRunnerMu sync.Mutex
	currentRunner   = godeps.DefaultRunner
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

func getRunner() CommandRunner {
	defaultRunnerMu.Lock()
	defer defaultRunnerMu.Unlock()
	if currentRunner == nil {
		return godeps.DefaultRunner
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
		Installer:    &ytDlpInstaller{},
	},
	{
		Name:         "ffmpeg",
		MinVersion:   "4.4",
		InstallURL:   "https://ffmpeg.org/download.html",
		VersionArgs:  []string{"-version"},
		ParseVersion: ParseVersionOutput,
		Installer:    &pkgInstaller{pkgName: "ffmpeg"},
	},
	{
		Name:         "ffprobe",
		MinVersion:   "4.4",
		InstallURL:   "https://ffmpeg.org/download.html",
		VersionArgs:  []string{"-version"},
		ParseVersion: ParseVersionOutput,
		Installer:    &pkgInstaller{pkgName: "ffmpeg"},
	},
}

type ytDlpInstaller struct{}

func (y *ytDlpInstaller) Install(ctx context.Context, targetDir string) error {
	return godeps.InstallYtDlp(ctx, targetDir)
}

func (y *ytDlpInstaller) Update(ctx context.Context, targetDir string) error {
	return godeps.UpdateYtDlp(ctx, getRunner(), targetDir)
}

type pkgInstaller struct {
	pkgName string
}

func (p *pkgInstaller) Install(ctx context.Context, targetDir string) error {
	return godeps.InstallPackage(ctx, p.pkgName, getRunner())
}

func (p *pkgInstaller) Update(ctx context.Context, targetDir string) error {
	return godeps.InstallPackage(ctx, p.pkgName, getRunner())
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
	r := getRunner()
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
	return VerifyDependenciesWithRunner(ctx, getRunner(), c, RequiredDependencies...)
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
					return out, errors.New(cleanErr)
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

// InstallMissingDependencies installs all unsatisfied dependencies.
func InstallMissingDependencies(ctx context.Context) ([]string, error) {
	c, _ := cache.New(true)
	mgr := NewManager(c)
	return mgr.InstallUnsatisfied(ctx)
}

// UpdateAllDependencies updates all declared dependencies.
func UpdateAllDependencies(ctx context.Context) ([]string, error) {
	c, _ := cache.New(true)
	mgr := NewManager(c)
	return mgr.UpdateAll(ctx)
}

// InitManagedPath ensures the managed bin directory is prepended to process PATH.
func InitManagedPath() error {
	return godeps.InitManagedPath(AppName)
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
	return godeps.SanitizeStderr(msg)
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
