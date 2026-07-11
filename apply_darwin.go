//go:build darwin

package sparkle

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InstalledAppBundle returns the .app the running binary lives in, or "" when
// not launched from a bundle (a dev binary or CLI). Used by Apply.
func InstalledAppBundle() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	macos := filepath.Dir(exe)      // .../Contents/MacOS
	contents := filepath.Dir(macos) // .../Contents
	app := filepath.Dir(contents)   // .../Something.app
	if filepath.Base(macos) == "MacOS" && filepath.Base(contents) == "Contents" && strings.HasSuffix(app, ".app") {
		return app
	}
	return ""
}

// Apply installs a downloaded update: it extracts the new .app from the archive
// (a .zip or a .dmg), swaps it over the currently-running .app, and relaunches.
// Only works when the running binary lives in a writable .app. Returns the .app
// path.
func Apply(archivePath string) (string, error) {
	app := InstalledAppBundle()
	if app == "" {
		return "", fmt.Errorf("not running from an installed .app - reinstall from the download to enable in-place updates")
	}
	return ApplyArchiveTo(archivePath, app, true)
}

// ApplyArchiveTo extracts the .app from archivePath (a .zip or a .dmg) and swaps
// it over target, then (when relaunch) opens the new bundle. The swap is
// rename-based, so it is atomic and safe while the old bundle is still running:
// macOS keeps the running executable's inode alive after its path is renamed. A
// leftover <target>.bak is removed on the next launch by CleanupBackups.
func ApplyArchiveTo(archivePath, target string, relaunch bool) (string, error) {
	dir := filepath.Dir(target)
	if err := writableDir(dir); err != nil {
		return "", fmt.Errorf("%s is not writable (%w) - move the app to ~/Applications or run the installer", dir, err)
	}
	stage, err := os.MkdirTemp(dir, ".sparkle-update-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)

	newApp, err := extractApp(archivePath, stage)
	if err != nil {
		return "", err
	}

	bak := target + ".bak"
	_ = os.RemoveAll(bak)
	if err := os.Rename(target, bak); err != nil {
		return "", fmt.Errorf("moving old app aside: %w", err)
	}
	if err := os.Rename(newApp, target); err != nil {
		_ = os.Rename(bak, target) // roll back so the user keeps an app
		return "", fmt.Errorf("installing new app: %w", err)
	}
	_ = os.RemoveAll(bak)

	if relaunch {
		if err := exec.Command("open", target).Start(); err != nil {
			return target, fmt.Errorf("relaunching %s: %w", target, err)
		}
	}
	return target, nil
}

// CleanupBackups removes a leftover <app>.bak from a prior in-place update.
// Pass the installed bundle (or "" to resolve it). Best-effort.
func CleanupBackups(app string) {
	if app == "" {
		app = InstalledAppBundle()
	}
	if app != "" {
		_ = os.RemoveAll(app + ".bak")
	}
}

func writableDir(dir string) error {
	probe := filepath.Join(dir, ".sparkle-write-probe")
	f, err := os.Create(probe)
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(probe)
}

// findDotApp returns the single *.app directory directly under root.
func findDotApp(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".app") {
			return filepath.Join(root, e.Name()), nil
		}
	}
	return "", fmt.Errorf("update archive contains no .app bundle")
}

// extractApp places the update's .app into stage and returns its path,
// dispatching by content: a ZIP (magic "PK") is ditto-extracted; anything else
// is treated as a disk image and mounted (Sparkle enclosures can be .dmg). The
// download's file name is unreliable (it is often "*.zip" regardless), so the
// format is sniffed from the bytes, not the extension.
func extractApp(archivePath, stage string) (string, error) {
	zip, err := looksLikeZip(archivePath)
	if err != nil {
		return "", err
	}
	if zip {
		// ditto preserves code signatures, symlinks, and xattrs a plain unzip
		// mangles - the extracted .app must stay launchable.
		if out, err := exec.Command("ditto", "-x", "-k", archivePath, stage).CombinedOutput(); err != nil {
			return "", fmt.Errorf("unpacking update: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return findDotApp(stage)
	}
	return extractAppFromDMG(archivePath, stage)
}

// looksLikeZip reports whether the file begins with the ZIP local-file magic
// ("PK", covering PK\x03\x04, PK\x05\x06, PK\x07\x08). A DMG never does.
func looksLikeZip(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	var hdr [2]byte
	n, err := io.ReadFull(f, hdr[:])
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return false, nil // too short to be a zip
	}
	if err != nil {
		return false, err
	}
	return n == 2 && hdr[0] == 'P' && hdr[1] == 'K', nil
}

// extractAppFromDMG mounts a disk image read-only, copies the .app off the
// mounted volume into stage (ditto, to preserve the bundle), and detaches. It
// works with any image hdiutil can attach (UDIF/UDZO/raw).
//
// It mounts with -mountrandom at a private temp dir rather than /Volumes: an
// app's DMG volume name (e.g. "My App") often collides with a copy the user
// already has open in Finder, and attaching over that either fails or lands at
// "/Volumes/My App 1". A private mount point sidesteps the collision entirely
// and keeps the mount out of /Volumes.
func extractAppFromDMG(dmgPath, stage string) (string, error) {
	mountRoot, err := os.MkdirTemp("", "sparkle-dmg-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(mountRoot) // runs after the detach below (LIFO)

	out, err := exec.Command("hdiutil", "attach",
		"-nobrowse", "-readonly", "-noverify", "-noautoopen",
		"-mountrandom", mountRoot, dmgPath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mounting disk image: %v: %s", err, strings.TrimSpace(string(out)))
	}
	mounts := parseHdiutilMounts(string(out))
	if len(mounts) == 0 {
		return "", fmt.Errorf("disk image mounted no volumes")
	}
	// Detach every volume we attached, force so a transient lock cannot leave
	// it mounted.
	defer func() {
		for _, m := range mounts {
			_ = exec.Command("hdiutil", "detach", "-quiet", "-force", m).Run()
		}
	}()

	var appSrc string
	for _, m := range mounts {
		if a, e := findDotApp(m); e == nil {
			appSrc = a
			break
		}
	}
	if appSrc == "" {
		return "", fmt.Errorf("disk image contains no .app bundle")
	}
	dst := filepath.Join(stage, filepath.Base(appSrc))
	if out, err := exec.Command("ditto", appSrc, dst).CombinedOutput(); err != nil {
		return "", fmt.Errorf("copying app out of disk image: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return dst, nil
}

// parseHdiutilMounts extracts mount points from `hdiutil attach` output. Columns
// are tab-separated; the mount point is the trailing field (an absolute path
// that is not a /dev node). Works for both /Volumes and -mountrandom locations,
// and tolerates volume names containing spaces.
func parseHdiutilMounts(out string) []string {
	var mounts []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		mp := strings.TrimRight(fields[len(fields)-1], " \t\r")
		if strings.HasPrefix(mp, "/") && !strings.HasPrefix(mp, "/dev/") {
			mounts = append(mounts, mp)
		}
	}
	return mounts
}
