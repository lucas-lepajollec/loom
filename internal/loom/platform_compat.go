package loom

import (
	"context"
	"os/exec"

	"github.com/lucas-lepajollec/loom/internal/loom/platform"
)

// Compatibilité pendant la migration par feuilles : les anciens noms restent
// dans loom, les primitives OS autonomes vivent dans platform. La configuration,
// les sessions et l'orchestration des services restent à leurs propriétaires.
// Aucun alias de fonction variable ni nouvel état global n'est introduit.
const (
	uiUnitName            = platform.UIUnitName
	trayIconSize          = platform.TrayIconSize
	createNewProcessGroup = platform.CreateNewProcessGroup
	detachedProcess       = platform.DetachedProcess
	createNoWindow        = platform.CreateNoWindow
)

type ghRelease = platform.Release

func defaultLoomHome() string           { return platform.DefaultLoomHome() }
func defaultEditor() string             { return platform.DefaultEditor() }
func hideCmd(cmd *exec.Cmd) *exec.Cmd   { return platform.HideCmd(cmd) }
func openBrowser(url string) error      { return platform.OpenBrowser(url) }
func totalRAMGB() float64               { return platform.TotalRAMGB() }
func autoInstallTool(name string) error { return platform.AutoInstallTool(name) }
func refreshToolPath()                  { platform.RefreshToolPath() }
func cudaPathEnv(dir string) []string   { return platform.CudaPathEnv(dir) }
func execServer(bin string, args []string) error {
	return platform.ExecServer(bin, args)
}
func newShellCmd(ctx context.Context, command string) (*exec.Cmd, func()) {
	return platform.NewShellCmd(ctx, command)
}
func ramUsageMB() (int, int) { return platform.RamUsageMB() }
func spawnDetached(name string, args ...string) *exec.Cmd {
	return platform.SpawnDetached(name, args...)
}
func pidAlive(pid int) bool                  { return platform.PidAlive(pid) }
func processAlive(pid int) bool              { return platform.ProcessAlive(pid) }
func killTree(pid int)                       { platform.KillTree(pid) }
func tailFile(path string, n int) string     { return platform.TailFile(path, n) }
func engineCmdlineOwned(cmdline string) bool { return platform.EngineCmdlineOwned(cmdline) }
func diskFreeAt(dir string) int64            { return platform.DiskFreeAt(dir) }
func setupConsole() bool                     { return platform.SetupConsole() }
func appWarning() string                     { return platform.AppWarning() }
func isTerminal() bool                       { return platform.IsTerminal() }
func defaultConfig() map[string]string       { return platform.DefaultConfig() }
func installedExePath() string               { return platform.InstalledExePath(binDir()) }
func BrandIconPNG(n int) []byte              { return platform.BrandIconPNG(n) }
func BrandICO(sizes ...int) []byte           { return platform.BrandICO(sizes...) }
func brandTemplatePNG(n int) []byte          { return platform.BrandTemplatePNG(n) }
func loomIcon(size int) []byte               { return platform.PWAIcon(size) }

type splash struct{ *platform.Splash }

func showSplash(text string) *splash { return &splash{platform.ShowSplash(text)} }
func (s *splash) close() {
	if s != nil {
		s.Close()
	}
}

// Le coupe-circuit de test du pare-feu reste dans loom, fourni explicitement
// aux opérations OS ; platform n'ajoute pas de global pour le reproduire.
func firewallOpen(port int) error        { return platform.FirewallOpen(port, firewallInert) }
func firewallClose(port int) error       { return platform.FirewallClose(port, firewallInert) }
func firewallState(port int) string      { return platform.FirewallState(port, firewallInert) }
func firewallManualHint(port int) string { return platform.FirewallManualHint(port) }

func updateAssetName() string                          { return platform.UpdateAssetName() }
func assetNameFor(goos, goarch string) string          { return platform.AssetNameFor(goos, goarch) }
func pickAsset(rel *ghRelease) (string, string, int64) { return platform.PickAsset(rel) }
func ensureV(s string) string                          { return platform.EnsureV(s) }
func fetchLatestRelease() (*ghRelease, error)          { return platform.FetchRelease(updateChannel()) }
func updatePermissionError(exe string) error           { return platform.UpdatePermissionError(exe) }
func checkUpdateWritable(exe string) error             { return platform.CheckUpdateWritable(exe) }
func downloadTo(url, dst string) error                 { return platform.DownloadTo(url, dst) }
func verifyChecksum(rel *ghRelease, asset, path string) error {
	return platform.VerifyChecksum(rel, asset, path)
}
func fileSize(path string) int64              { return platform.FileSize(path) }
func replaceBinary(exe, tmp string) error     { return platform.ReplaceBinary(exe, tmp) }
func renameAside(path string) (string, error) { return platform.RenameAside(path) }
func removeOldBinaries(path string)           { platform.RemoveOldBinaries(path) }
func cleanupOldBinary()                       { platform.CleanupOldBinary() }

func uiServiceName() string     { return platform.UIServiceName() }
func uiServiceActive() bool     { return platform.UIServiceActive() }
func cmdUI(args []string) error { return platform.CmdUI(args) }
