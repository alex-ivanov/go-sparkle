//go:build darwin

package sparkle

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ApplyArchiveTo must replace an installed .app's contents in place from a
// ditto-zipped bundle (the shape make-appcast produces), leaving no backup.
func TestApplyArchiveToSwapsZip(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "Applications")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(installDir, "App.app")
	writeApp(t, app, "OLD")

	stage := t.TempDir()
	newApp := filepath.Join(stage, "App.app")
	writeApp(t, newApp, "NEW")
	zip := filepath.Join(stage, "update.zip")
	if out, err := exec.Command("ditto", "-c", "-k", "--keepParent", newApp, zip).CombinedOutput(); err != nil {
		t.Fatalf("ditto zip: %v: %s", err, out)
	}

	assertSwaps(t, zip, app)
}

// ApplyArchiveTo must also install from a .dmg (Sparkle's other common
// enclosure), detected by content and mounted via hdiutil.
func TestApplyArchiveToSwapsDMG(t *testing.T) {
	if testing.Short() {
		t.Skip("hdiutil create/attach/detach round-trip is slow")
	}
	root := t.TempDir()
	installDir := filepath.Join(root, "Applications")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(installDir, "App.app")
	writeApp(t, app, "OLD")

	// Build a "new" App.app inside a source folder and wrap it in a DMG whose
	// root volume holds the bundle.
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeApp(t, filepath.Join(src, "App.app"), "NEW")
	dmg := makeDMG(t, "App", src)

	// Sanity: a real DMG must not be sniffed as a zip.
	if z, _ := looksLikeZip(dmg); z {
		t.Fatal("DMG mis-detected as a zip")
	}
	assertSwaps(t, dmg, app)
}

// A DMG whose volume name collides with one the user already has mounted (the
// classic hdiutil pitfall) must still apply, because we mount privately with
// -mountrandom instead of at /Volumes/<name>.
func TestApplyArchiveToDMGVolumeNameCollision(t *testing.T) {
	if testing.Short() {
		t.Skip("hdiutil create/attach/detach round-trips are slow")
	}
	const vol = "SparkleGoUpdateTest" // unlikely to exist on the host

	// A decoy DMG with the SAME volume name, mounted normally at /Volumes/<vol>.
	decoySrc := filepath.Join(t.TempDir(), "decoy")
	os.MkdirAll(decoySrc, 0o755)
	os.WriteFile(filepath.Join(decoySrc, "placeholder"), []byte("x"), 0o644)
	decoy := makeDMG(t, vol, decoySrc)
	out, err := exec.Command("hdiutil", "attach", "-nobrowse", "-noautoopen", decoy).CombinedOutput()
	if err != nil {
		t.Fatalf("mount decoy: %v: %s", err, out)
	}
	decoyMounts := parseHdiutilMounts(string(out))
	t.Cleanup(func() {
		for _, m := range decoyMounts {
			_ = exec.Command("hdiutil", "detach", "-quiet", "-force", m).Run()
		}
	})
	if len(decoyMounts) == 0 {
		t.Fatal("decoy did not mount (cannot exercise the collision)")
	}

	// The update DMG shares the volume name; Apply must still succeed.
	updSrc := filepath.Join(t.TempDir(), "upd")
	os.MkdirAll(updSrc, 0o755)
	writeApp(t, filepath.Join(updSrc, "App.app"), "NEW")
	upd := makeDMG(t, vol, updSrc)

	root := t.TempDir()
	installDir := filepath.Join(root, "Applications")
	os.MkdirAll(installDir, 0o755)
	app := filepath.Join(installDir, "App.app")
	writeApp(t, app, "OLD")
	assertSwaps(t, upd, app)
}

// makeDMG wraps srcDir into a compressed DMG with the given volume name.
func makeDMG(t *testing.T, volname, srcDir string) string {
	t.Helper()
	dmg := filepath.Join(t.TempDir(), "img.dmg")
	if out, err := exec.Command("hdiutil", "create", "-volname", volname, "-srcfolder", srcDir,
		"-fs", "HFS+", "-format", "UDZO", "-ov", dmg).CombinedOutput(); err != nil {
		t.Fatalf("hdiutil create: %v: %s", err, out)
	}
	return dmg
}

func TestApplyArchiveToRejectsArchiveWithoutApp(t *testing.T) {
	root := t.TempDir()
	plain := filepath.Join(root, "stuff")
	os.MkdirAll(plain, 0o755)
	os.WriteFile(filepath.Join(plain, "file"), []byte("x"), 0o644)
	zip := filepath.Join(root, "noapp.zip")
	if out, err := exec.Command("ditto", "-c", "-k", "--keepParent", plain, zip).CombinedOutput(); err != nil {
		t.Fatalf("ditto: %v: %s", err, out)
	}
	target := filepath.Join(root, "App.app")
	writeApp(t, target, "OLD")
	if _, err := ApplyArchiveTo(zip, target, false); err == nil {
		t.Fatal("archive without a .app should fail")
	}
	// The original app must survive a failed apply.
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("original app lost after failed apply: %v", err)
	}
}

// assertSwaps applies archive over app and checks the bundle was replaced
// (marker -> "NEW") with no leftover backup.
func assertSwaps(t *testing.T, archive, app string) {
	t.Helper()
	got, err := ApplyArchiveTo(archive, app, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != app {
		t.Fatalf("ApplyArchiveTo returned %q, want %q", got, app)
	}
	marker, err := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "app"))
	if err != nil {
		t.Fatal(err)
	}
	if string(marker) != "NEW" {
		t.Fatalf("bundle not swapped: marker=%q", marker)
	}
	if _, err := os.Stat(app + ".bak"); !os.IsNotExist(err) {
		t.Fatal("leftover .bak not cleaned up")
	}
}

func writeApp(t *testing.T, app, marker string) {
	t.Helper()
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(macos, "app"), []byte(marker), 0o755); err != nil {
		t.Fatal(err)
	}
}
