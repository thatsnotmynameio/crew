package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// binaryMode is the mode of the crew upgrade installs, as the README's
// install command gives it.
const binaryMode = 0o755

// tempPattern names the temp files crew upgrade writes beside the binary,
// hidden so a killed upgrade leaves nothing on PATH.
const tempPattern = ".crew-upgrade-*"

// archiveName is the name of the release archive for goos and goarch, as
// .goreleaser.yaml names it.
func archiveName(goos, goarch string) string {
	return "crew_" + goos + "_" + goarch + ".tar.gz"
}

// Binary downloads the archive of rel for goos and goarch and its
// checksums.txt, checks the archive against it before it opens it, and
// returns the crew the archive holds.
func (c *Client) Binary(ctx context.Context, rel Release, goos, goarch string) ([]byte, error) {
	name := archiveName(goos, goarch)
	sums, err := c.Download(ctx, rel, "checksums.txt", maxSums)
	if err != nil {
		return nil, err
	}
	data, err := c.Download(ctx, rel, name, maxArchive)
	if err != nil {
		return nil, err
	}
	if err := verify(data, sums, name); err != nil {
		return nil, fmt.Errorf("release %s: %w", rel.Tag, err)
	}
	crew, err := extract(data, name, maxArchive)
	if err != nil {
		return nil, fmt.Errorf("release %s: %w", rel.Tag, err)
	}
	return crew, nil
}

// verify checks data, the archive named name, against sums, a release's
// checksums.txt, which must hold exactly one SHA-256 line for name.
func verify(data, sums []byte, name string) error {
	var want []string
	for line := range strings.Lines(string(sums)) {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			want = append(want, fields[0])
		}
	}
	switch len(want) {
	case 0:
		return fmt.Errorf("checksums.txt has no checksum for %s", name)
	case 1:
	default:
		return fmt.Errorf("checksums.txt has %d checksums for %s", len(want), name)
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(want[0], hex.EncodeToString(sum[:])) {
		return fmt.Errorf("%s does not match checksums.txt: checksum mismatch", name)
	}
	return nil
}

// extract returns the crew binary data, the tar.gz named name, holds: its
// one regular file named crew at the archive's root, refusing one larger
// than limit bytes. A header's name is compared, never joined into a path.
func extract(data []byte, name string, limit int64) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s is not a tar.gz: %w", name, err)
	}
	tr := tar.NewReader(gz)
	var crew []byte
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		if path.Clean(hdr.Name) != "crew" {
			continue
		}
		if crew != nil {
			return nil, fmt.Errorf("%s holds more than one crew", name)
		}
		if crew, err = readCrew(tr, hdr, name, limit); err != nil {
			return nil, err
		}
	}
	if crew == nil {
		return nil, fmt.Errorf("%s holds no crew", name)
	}
	return crew, nil
}

// readCrew reads the crew entry hdr of tr, the archive named name, which
// must be a regular file of at most limit bytes.
func readCrew(tr *tar.Reader, hdr *tar.Header, name string, limit int64) ([]byte, error) {
	if hdr.Typeflag != tar.TypeReg {
		return nil, fmt.Errorf("%s's crew is not a regular file", name)
	}
	crew, err := io.ReadAll(io.LimitReader(tr, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	if int64(len(crew)) > limit {
		return nil, fmt.Errorf("%s's crew is larger than %d bytes", name, limit)
	}
	return crew, nil
}

// Target returns the path of the running crew, which executable returns
// (os.Executable), with its symlinks resolved, so a symlinked install
// keeps its link. When crew cannot find it, it returns an EnvError.
func Target(executable func() (string, error)) (string, error) {
	exe, err := executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return "", envErrorf("crew cannot find its own path: %w", err)
	}
	return exe, nil
}

// CheckWritable returns an EnvError when crew cannot replace target, the
// binary to replace, by creating and removing a temp file beside it.
func CheckWritable(target string) error {
	f, err := os.CreateTemp(filepath.Dir(target), tempPattern)
	if err != nil {
		return writeError(target, err)
	}
	name := f.Name()
	return errors.Join(f.Close(), os.Remove(name))
}

// Replace writes binary to a temp file beside target and renames it over
// target, so target is never written in place: a crew running the old file
// keeps it, and target holds the old crew or the new one, never part of
// either. On any failure, or once ctx is done, it removes the temp file and
// leaves target as it was; a done ctx is ErrInterrupted.
func Replace(ctx context.Context, target string, binary []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(target), tempPattern)
	if err != nil {
		return writeError(target, err)
	}
	tmp := f.Name()
	renamed := false
	defer func() {
		if !renamed {
			err = errors.Join(err, removeTemp(tmp))
		}
	}()
	if err := writeBinary(f, binary); err != nil {
		return writeError(target, err)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %w; crew is unchanged at %s", ErrInterrupted, context.Cause(ctx), target)
	}
	if err := os.Rename(tmp, target); err != nil {
		return writeError(target, err)
	}
	renamed = true
	syncDir(filepath.Dir(target))
	return nil
}

// writeBinary writes binary to f, makes it executable and syncs it, all
// through the open file, then closes it.
func writeBinary(f *os.File, binary []byte) error {
	_, err := f.Write(binary)
	if err == nil {
		err = f.Chmod(binaryMode)
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

// removeTemp removes tmp, a temp file Replace wrote.
func removeTemp(tmp string) error {
	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing %s: %w", tmp, err)
	}
	return nil
}

// syncDir syncs dir, so the rename survives a power loss. It is best
// effort: the new crew is already installed.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil { //nolint:gosec // G304: the directory of the running crew
		_ = d.Sync()
		_ = d.Close()
	}
}

// writeError returns the error of a failed write beside target: an
// EnvError pointing to the README's install into ~/.local/bin when crew
// may not write there, and one naming target otherwise.
func writeError(target string, err error) error {
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS) {
		return envErrorf("crew cannot replace %s (%w), and crew upgrade never uses sudo; "+
			"install crew into ~/.local/bin with the README's install command, then remove %s",
			target, err, target)
	}
	return fmt.Errorf("crew could not write the new crew at %s: %w", target, err)
}
