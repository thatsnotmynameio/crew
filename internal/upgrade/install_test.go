package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// archiveOf is the archive the tests' release holds for linux/amd64.
const archiveOf = "crew_linux_amd64.tar.gz"

// entry is a file of a test archive.
type entry struct {
	name     string
	body     string
	typeflag byte
	link     string
}

// archive returns a tar.gz holding entries.
func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Mode: 0o755, Typeflag: typeflag, Linkname: e.link}
		if typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// goodArchive is a release archive as GoReleaser writes it, whose crew
// is the new crew.
func goodArchive(t *testing.T) []byte {
	t.Helper()
	return archive(t,
		entry{name: "LICENSE", body: "MIT"}, entry{name: "crew", body: "new crew"}, entry{name: "README.md", body: "# crew"})
}

// sumsOf returns a checksums.txt with a line for another archive and one
// for data, named archiveOf.
func sumsOf(data []byte) []byte {
	sum := sha256.Sum256(data)
	return []byte(zeroSum + "  crew_darwin_arm64.tar.gz\n" + hex.EncodeToString(sum[:]) + "  " + archiveOf + "\n")
}

// zeroSum is a SHA-256 no archive has.
var zeroSum = strings.Repeat("0", 64)

// installed writes an old crew binary into a temp dir and returns its
// path and what it is, for unchanged.
func installed(t *testing.T) (string, os.FileInfo) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crew")
	if err := os.WriteFile(path, []byte("old crew"), 0o700); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, fi
}

// unchanged fails the test unless path is still before, the same file
// holding the old crew, with no temp file beside it.
func unchanged(t *testing.T, path string, before os.FileInfo) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old crew" {
		t.Errorf("%s holds %q, %v, want the old crew", path, data, err)
	}
	if after, err := os.Stat(path); err != nil || !os.SameFile(before, after) {
		t.Errorf("%s is no longer the old file: %v", path, err)
	}
	noTemp(t, filepath.Dir(path))
}

// noTemp fails the test when dir holds anything but crew.
func noTemp(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "crew" {
			t.Errorf("%s holds %s, want only crew", dir, e.Name())
		}
	}
}

// readOnly makes the directory of path read-only until the test ends, or
// skips the test under root, which writes any directory.
func readOnly(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes any directory")
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // G302: a test directory made read-only
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // G302: the test's own directory
}

// checkCannotReplace fails the test unless err is the EnvError of a path
// crew may not write, which points to the README's install.
func checkCannotReplace(t *testing.T, err error, path string) {
	t.Helper()
	if _, ok := errors.AsType[*EnvError](err); !ok {
		t.Errorf("err = %v, want an *EnvError", err)
		return
	}
	for _, want := range []string{"crew cannot replace " + path, "never uses sudo", "~/.local/bin", "remove " + path} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to say %q", err, want)
		}
	}
}

func TestArchiveNameIsThePlatformsGoReleaserArchive(t *testing.T) {
	if got := archiveName("darwin", "arm64"); got != "crew_darwin_arm64.tar.gz" {
		t.Errorf("archiveName = %q", got)
	}
}

func TestVerifyAcceptsTheArchiveItsLineNames(t *testing.T) {
	data := goodArchive(t)
	if err := verify(data, sumsOf(data), archiveOf); err != nil {
		t.Errorf("verify = %v", err)
	}
}

func TestVerifyRefusesAMismatchOrAMissingOrDoubleLine(t *testing.T) {
	data := goodArchive(t)
	tampered := bytes.Clone(data)
	tampered[len(tampered)/2] ^= 1
	sums := string(sumsOf(data))
	tests := []struct {
		name string
		data []byte
		sums string
		want string
	}{
		{name: "one byte changed", data: tampered, sums: sums,
			want: archiveOf + " does not match checksums.txt: checksum mismatch"},
		{name: "no line", data: data, sums: zeroSum + "  crew_darwin_arm64.tar.gz\n",
			want: "checksums.txt has no checksum for " + archiveOf},
		{name: "two lines", data: data, sums: sums + sums, want: "checksums.txt has 2 checksums for " + archiveOf},
		{name: "a line naming a longer file", data: data, sums: zeroSum + "  x" + archiveOf + "\n",
			want: "no checksum for " + archiveOf},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verify(tt.data, []byte(tt.sums), archiveOf)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("verify = %v, want an error saying %q", err, tt.want)
			}
		})
	}
}

func TestExtractTakesOnlyTheRegularCrew(t *testing.T) {
	got, err := extract(goodArchive(t), archiveOf, maxArchive)
	if err != nil || string(got) != "new crew" {
		t.Errorf("extract = %q, %v, want the new crew", got, err)
	}
	got, err = extract(archive(t, entry{name: "./crew", body: "dotted"}), archiveOf, maxArchive)
	if err != nil || string(got) != "dotted" {
		t.Errorf("extract of ./crew = %q, %v", got, err)
	}
}

func TestExtractRefusesAnArchiveWithoutOneRegularCrew(t *testing.T) {
	tests := []struct {
		name    string
		entries []entry
		want    string
	}{
		{name: "no crew", entries: []entry{{name: "LICENSE", body: "MIT"}}, want: archiveOf + " holds no crew"},
		{name: "symlink", entries: []entry{{name: "crew", typeflag: tar.TypeSymlink, link: "/bin/sh"}},
			want: archiveOf + "'s crew is not a regular file"},
		{name: "hard link", entries: []entry{
			{name: "LICENSE", body: "MIT"}, {name: "crew", typeflag: tar.TypeLink, link: "LICENSE"},
		},
			want: "not a regular file"},
		{name: "two crews", entries: []entry{{name: "crew", body: "a"}, {name: "crew", body: "b"}},
			want: archiveOf + " holds more than one crew"},
		{name: "traversal", entries: []entry{{name: "../crew", body: "a"}}, want: "holds no crew"},
		{name: "nested", entries: []entry{{name: "bin/crew", body: "a"}}, want: "holds no crew"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extract(archive(t, tt.entries...), archiveOf, maxArchive)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("extract = %v, want an error saying %q", err, tt.want)
			}
		})
	}
}

func TestExtractRefusesAnArchiveThatIsNotATarGz(t *testing.T) {
	if _, err := extract([]byte("not gzip"), archiveOf, maxArchive); err == nil ||
		!strings.Contains(err.Error(), archiveOf+" is not a tar.gz") {
		t.Errorf("extract of garbage = %v", err)
	}
	// A gzip stream cut short fails while crew reads the tar it holds.
	data := goodArchive(t)
	_, err := extract(data[:len(data)/2], archiveOf, maxArchive)
	if err == nil || !strings.Contains(err.Error(), "reading "+archiveOf) || strings.Contains(err.Error(), "GitHub") {
		t.Errorf("extract of a cut archive = %v, want a read error that blames no network", err)
	}
}

func TestExtractRefusesACrewOverTheCap(t *testing.T) {
	big := strings.Repeat("a", 2048)
	_, err := extract(archive(t, entry{name: "crew", body: big}), archiveOf, 1024)
	if err == nil || !strings.Contains(err.Error(), archiveOf+"'s crew is larger than 1024 bytes") {
		t.Errorf("extract over the cap = %v", err)
	}
	if got, err := extract(archive(t, entry{name: "crew", body: big}), archiveOf, 2048); err != nil || len(got) != 2048 {
		t.Errorf("extract at the cap = %d bytes, %v", len(got), err)
	}
}

func TestReplaceRenamesTheNewCrewOverTheOld(t *testing.T) {
	path, before := installed(t)
	// A crew that runs the old file holds it open, as this does.
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Close() })

	if err := Replace(t.Context(), path, []byte("new crew")); err != nil {
		t.Fatalf("Replace = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new crew" {
		t.Errorf("%s holds %q, %v, want the new crew", path, data, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != 0o755 {
		t.Errorf("%s has mode %v, want 0755", path, after.Mode().Perm())
	}
	if os.SameFile(before, after) {
		t.Error("Replace wrote over the old file, want a new one renamed over it")
	}
	kept, err := io.ReadAll(old)
	if err != nil || string(kept) != "old crew" {
		t.Errorf("the open old file reads %q, %v, want the old crew", kept, err)
	}
	noTemp(t, filepath.Dir(path))
}

func TestReplaceInAReadOnlyDirectoryChangesNothing(t *testing.T) {
	path, before := installed(t)
	readOnly(t, path)
	checkCannotReplace(t, Replace(t.Context(), path, []byte("new crew")), path)
	unchanged(t, path, before)
}

func TestCheckWritableInAReadOnlyDirectoryChangesNothing(t *testing.T) {
	path, before := installed(t)
	readOnly(t, path)
	checkCannotReplace(t, CheckWritable(path), path)
	unchanged(t, path, before)
}

func TestCheckWritableLeavesNothingBehind(t *testing.T) {
	path, before := installed(t)
	if err := CheckWritable(path); err != nil {
		t.Fatalf("CheckWritable = %v", err)
	}
	unchanged(t, path, before)
}

func TestReplaceCancelledChangesNothing(t *testing.T) {
	path, before := installed(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Replace(ctx, path, []byte("new crew"))
	if !errors.Is(err, ErrInterrupted) || !errors.Is(err, context.Canceled) {
		t.Errorf("Replace = %v, want ErrInterrupted", err)
	}
	if err == nil || !strings.Contains(err.Error(), "crew is unchanged at "+path) {
		t.Errorf("Replace = %v, want it to say crew is unchanged at %s", err, path)
	}
	unchanged(t, path, before)
}

func TestReplaceThatCannotRenameChangesNothing(t *testing.T) {
	// A directory where the binary should be: the rename fails for a
	// reason other than permission.
	dir := filepath.Join(t.TempDir(), "crew")
	if err := os.MkdirAll(filepath.Join(dir, "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := Replace(t.Context(), dir, []byte("new crew"))
	if _, ok := errors.AsType[*EnvError](err); ok || err == nil || !strings.Contains(err.Error(), dir) {
		t.Errorf("Replace = %v, want an error naming %s that is not an EnvError", err, dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("%s changed: %v, %v", dir, fi, err)
	}
	noTemp(t, filepath.Dir(dir))
}

func TestReplaceInAMissingDirectoryNamesThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gone", "crew")
	err := Replace(t.Context(), path, []byte("new crew"))
	_, env := errors.AsType[*EnvError](err)
	if env || !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), path) {
		t.Errorf("Replace = %v, want a not-exist error naming %s", err, path)
	}
}

func TestTargetResolvesASymlinkedInstall(t *testing.T) {
	path, _ := installed(t)
	link := filepath.Join(t.TempDir(), "crew")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	target, err := Target(func() (string, error) { return link, nil })
	if err != nil || target != path {
		t.Fatalf("Target = %q, %v, want %q", target, err, path)
	}
	if err := Replace(t.Context(), target, []byte("new crew")); err != nil {
		t.Fatalf("Replace = %v", err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link is gone: %v, %v", fi, err)
	}
	data, err := os.ReadFile(link)
	if err != nil || string(data) != "new crew" {
		t.Errorf("the link reads %q, %v, want the new crew", data, err)
	}
}

func TestTargetThatCannotFindTheRunningCrewIsAnEnvError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "crew")
	for name, executable := range map[string]func() (string, error){
		"os.Executable fails": func() (string, error) { return "", errors.New("no /proc") },
		"a path that is gone": func() (string, error) { return missing, nil },
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Target(executable)
			if _, ok := errors.AsType[*EnvError](err); !ok || !strings.Contains(err.Error(), "crew cannot find its own path") {
				t.Errorf("Target = %v, want an EnvError saying crew cannot find its own path", err)
			}
		})
	}
}

// releaseWith returns a fake GitHub serving one release, v0.5.0, of
// assets, and the release as a client of it finds it.
func releaseWith(t *testing.T, assets map[string][]byte) (*Client, Release) {
	t.Helper()
	rel := &fakeRelease{tag: "v0.5.0", assets: assets}
	f := &fakeGitHub{t: t, latest: rel, releases: []*fakeRelease{rel}}
	c := f.serve("")
	release, err := c.Release(t.Context(), "")
	if err != nil {
		t.Fatalf("Release = %v", err)
	}
	return c, release
}

func TestBinaryReturnsTheCrewOfTheCheckedArchive(t *testing.T) {
	data := goodArchive(t)
	c, rel := releaseWith(t, map[string][]byte{"checksums.txt": sumsOf(data), archiveOf: data})
	got, err := c.Binary(t.Context(), rel, "linux", "amd64")
	if err != nil || string(got) != "new crew" {
		t.Errorf("Binary = %q, %v, want the new crew", got, err)
	}
}

func TestBinaryRefusesAnArchiveItCannotCheckOrOpen(t *testing.T) {
	data := goodArchive(t)
	tampered := bytes.Clone(data)
	tampered[len(tampered)/2] ^= 1
	noCrew := archive(t, entry{name: "LICENSE", body: "MIT"})
	tests := []struct {
		name   string
		assets map[string][]byte
		want   string
	}{
		{name: "checksum mismatch", assets: map[string][]byte{"checksums.txt": sumsOf(data), archiveOf: tampered},
			want: "release v0.5.0: " + archiveOf + " does not match checksums.txt: checksum mismatch"},
		{name: "no checksums.txt", assets: map[string][]byte{archiveOf: data}, want: "release v0.5.0 has no checksums.txt"},
		{name: "no archive", assets: map[string][]byte{"checksums.txt": sumsOf(data)},
			want: "release v0.5.0 has no " + archiveOf},
		{name: "no line", assets: map[string][]byte{"checksums.txt": []byte(zeroSum + "  other\n"), archiveOf: data},
			want: "release v0.5.0: checksums.txt has no checksum for " + archiveOf},
		{name: "no crew", assets: map[string][]byte{"checksums.txt": sumsOf(noCrew), archiveOf: noCrew},
			want: "release v0.5.0: " + archiveOf + " holds no crew"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rel := releaseWith(t, tt.assets)
			_, err := c.Binary(t.Context(), rel, "linux", "amd64")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Binary = %v, want an error saying %q", err, tt.want)
			}
		})
	}
}
