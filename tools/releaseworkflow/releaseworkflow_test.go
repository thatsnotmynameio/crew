// Package releaseworkflow tests .github/workflows/release.yml: the scripts
// its build and publish jobs run inline, by running them as the workflow
// holds them against a fake dist folder and a stub gh, and the order and
// permissions of its jobs.
package releaseworkflow

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// job is the part of a workflow job the tests read.
type job struct {
	Needs       []string          `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []struct {
		Name string `yaml:"name"`
		Run  string `yaml:"run"`
	} `yaml:"steps"`
}

// workflow is the part of a workflow file the tests read.
type workflow struct {
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]job    `yaml:"jobs"`
}

// build is one platform of the fake release: its archive, the folder
// GoReleaser leaves its binary in, and the binary's bytes.
type build struct {
	archive string
	folder  string
	binary  []byte
}

// builds are the fake release's platforms, named as GoReleaser names them.
var builds = []build{
	{"crew_darwin_amd64.tar.gz", "crew_darwin_amd64_v1", []byte("crew for darwin amd64")},
	{"crew_darwin_arm64.tar.gz", "crew_darwin_arm64_v8.0", []byte("crew for darwin arm64")},
	{"crew_linux_amd64.tar.gz", "crew_linux_amd64_v1", []byte("crew for linux amd64")},
	{"crew_linux_arm64.tar.gz", "crew_linux_arm64_v8.0", []byte("crew for linux arm64")},
}

// repository is the GITHUB_REPOSITORY the publish step runs in.
const repository = "thatsnotmynameio/crew"

// verifyFlags are the flags every gh attestation verify call must pass.
const verifyFlags = "--repo thatsnotmynameio/crew" +
	" --signer-workflow thatsnotmynameio/crew/.github/workflows/release.yml" +
	" --source-ref refs/heads/main"

// stubGH stands in for gh on PATH: it logs each call to GH_LOG, logs the
// sha256 of each file it verifies to GH_VERIFIED, and answers from the STUB_
// variables.
const stubGH = `#!/usr/bin/env bash
echo "$*" >> "$GH_LOG"
case "$1 $2" in
  "release view") echo "$STUB_DRAFT" ;;
  "release list") if [ -n "$STUB_PUBLISHED" ]; then printf '%s\n' $STUB_PUBLISHED; fi ;;
  "release download")
    dir=""
    while [ "$#" -gt 0 ]; do
      if [ "$1" = "--dir" ]; then dir="$2"; fi
      shift
    done
    mkdir -p "$dir"
    cp -R "$STUB_ASSETS/." "$dir/"
    ;;
  "attestation verify")
    digest="$(sha256sum "$3" | cut -d' ' -f1)"
    echo "$digest" >> "$GH_VERIFIED"
    if [ "$digest" = "$STUB_UNVERIFIED" ]; then
      echo "verification failed for $digest" >&2
      exit 1
    fi
    ;;
  "release edit") ;;
  *) echo "unexpected gh $*" >&2; exit 2 ;;
esac
`

// readWorkflow decodes release.yml.
func readWorkflow(t *testing.T) workflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read the workflow: %v", err)
	}
	var w workflow
	err = yaml.Unmarshal(data, &w)
	if err != nil {
		t.Fatalf("decode the workflow: %v", err)
	}

	return w
}

// step returns the run script of the step name in the job named.
func step(t *testing.T, jobName, name string) string {
	t.Helper()
	j, ok := readWorkflow(t).Jobs[jobName]
	if !ok {
		t.Fatalf("the workflow has no job %q", jobName)
	}
	for _, s := range j.Steps {
		if s.Name == name {
			return s.Run
		}
	}
	t.Fatalf("the %s job has no step %q", jobName, name)

	return ""
}

// runStep runs script in dir as GitHub runs a bash step, with env, and
// returns its exit code and output.
func runStep(t *testing.T, dir, script string, env ...string) (int, string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode(), stdout.String() + stderr.String()
	}
	if err != nil {
		t.Fatalf("run the step: %v", err)
	}

	return 0, stdout.String() + stderr.String()
}

// sum is the hex sha256 of data.
func sum(data []byte) string {
	digest := sha256.Sum256(data)

	return hex.EncodeToString(digest[:])
}

// writeFile writes data to path, creating its folder.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0o750)
	if err != nil {
		t.Fatalf("create the folder of %s: %v", path, err)
	}
	err = os.WriteFile(path, data, 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// file is one file of an archive.
type file struct {
	name string
	data []byte
}

// archive packs binary as crew, beside a LICENSE and a README.md, in a
// tar.gz, as GoReleaser does.
func archive(t *testing.T, binary []byte) []byte {
	t.Helper()

	return pack(t, file{"LICENSE", []byte("MIT")}, file{"README.md", []byte("# crew")}, file{"crew", binary})
}

// pack packs files in a tar.gz.
func pack(t *testing.T, files ...file) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data))})
		if err != nil {
			t.Fatalf("write the header of %s: %v", f.name, err)
		}
		_, err = tw.Write(f.data)
		if err != nil {
			t.Fatalf("write %s: %v", f.name, err)
		}
	}
	err := tw.Close()
	if err != nil {
		t.Fatalf("close the tar: %v", err)
	}
	err = gz.Close()
	if err != nil {
		t.Fatalf("close the gzip: %v", err)
	}

	return buf.Bytes()
}

// releaseArchives packs each build's binary in its archive and returns the
// archives' bytes by name.
func releaseArchives(t *testing.T) map[string][]byte {
	t.Helper()
	archives := map[string][]byte{}
	for _, b := range builds {
		archives[b.archive] = archive(t, b.binary)
	}

	return archives
}

// checksumLines are the lines of checksums.txt for archives, as GoReleaser
// writes them.
func checksumLines(archives map[string][]byte) []string {
	lines := make([]string, 0, len(builds))
	for _, b := range builds {
		lines = append(lines, sum(archives[b.archive])+"  "+b.archive)
	}

	return lines
}

// binaryLines are the subjects' lines for the binaries of bs, each named by
// its path relative to dist/.
func binaryLines(bs []build) []string {
	lines := make([]string, 0, len(bs))
	for _, b := range bs {
		lines = append(lines, sum(b.binary)+"  "+b.folder+"/crew")
	}

	return lines
}

// subjectsFile is the subjects file the build job writes for archives: the
// lines of checksums.txt, then one line per binary.
func subjectsFile(archives map[string][]byte) string {
	return strings.Join(append(checksumLines(archives), binaryLines(builds)...), "\n") + "\n"
}

// writeArtifacts writes dist/artifacts.json, listing the archives and the
// binaries of bs.
func writeArtifacts(t *testing.T, dir string, bs []build) {
	t.Helper()
	type artifact struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	artifacts := make([]artifact, 0, len(bs)+len(builds)+2)
	artifacts = append(artifacts, artifact{"metadata.json", "dist/metadata.json", "Metadata"})
	for _, b := range bs {
		artifacts = append(artifacts, artifact{"crew", "dist/" + b.folder + "/crew", "Binary"})
	}
	for _, b := range builds {
		artifacts = append(artifacts, artifact{b.archive, "dist/" + b.archive, "Archive"})
	}
	artifacts = append(artifacts, artifact{"checksums.txt", "dist/checksums.txt", "Checksum"})
	data, err := json.Marshal(artifacts)
	if err != nil {
		t.Fatalf("encode artifacts.json: %v", err)
	}
	writeFile(t, filepath.Join(dir, "dist", "artifacts.json"), data)
}

// makeDist writes the dist folder GoReleaser leaves in dir: each binary in
// its folder, the archives, checksums.txt and artifacts.json. It returns
// the archives' bytes by name.
func makeDist(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	archives := releaseArchives(t)
	for _, b := range builds {
		writeFile(t, filepath.Join(dir, "dist", b.folder, "crew"), b.binary)
		writeFile(t, filepath.Join(dir, "dist", b.archive), archives[b.archive])
	}
	checksums := strings.Join(checksumLines(archives), "\n") + "\n"
	writeFile(t, filepath.Join(dir, "dist", "checksums.txt"), []byte(checksums))
	writeArtifacts(t, dir, builds)

	return archives
}

// runSubjects runs the build job's subjects step in dir and returns its exit
// code, its output and the subjects file it wrote, if any.
func runSubjects(t *testing.T, dir string) (int, string, string) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not installed")
	}
	temp := t.TempDir()
	code, out := runStep(t, dir, step(t, "build", "subjects"), "RUNNER_TEMP="+temp)
	data, err := os.ReadFile(filepath.Join(temp, "subjects.txt"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read the subjects: %v", err)
	}

	return code, out, string(data)
}

func TestSubjectsListTheArchivesThenTheirBinaries(t *testing.T) {
	dir := t.TempDir()
	archives := makeDist(t, dir)
	code, out, subjects := runSubjects(t, dir)
	if code != 0 {
		t.Fatalf("the subjects step exited %d, output %q; want 0", code, out)
	}
	want := subjectsFile(archives)
	if subjects != want {
		t.Errorf("subjects\n%s\nwant\n%s", subjects, want)
	}
}

func TestSubjectsFailWithoutABinary(t *testing.T) {
	dir := t.TempDir()
	makeDist(t, dir)
	writeArtifacts(t, dir, nil)
	code, out, _ := runSubjects(t, dir)
	want := "dist/ has 4 archives and 0 binaries"
	if code == 0 || !strings.Contains(out, want) {
		t.Errorf("the subjects step exited %d, output %q; want a failure saying %q", code, out, want)
	}
}

func TestSubjectsFailWithFewerBinariesThanArchives(t *testing.T) {
	dir := t.TempDir()
	makeDist(t, dir)
	writeArtifacts(t, dir, builds[:3])
	code, out, _ := runSubjects(t, dir)
	want := "dist/ has 4 archives and 3 binaries"
	if code == 0 || !strings.Contains(out, want) {
		t.Errorf("the subjects step exited %d, output %q; want a failure saying %q", code, out, want)
	}
}

func TestSubjectsFailWithoutChecksums(t *testing.T) {
	dir := t.TempDir()
	makeDist(t, dir)
	err := os.Remove(filepath.Join(dir, "dist", "checksums.txt"))
	if err != nil {
		t.Fatalf("remove checksums.txt: %v", err)
	}
	code, out, _ := runSubjects(t, dir)
	if code == 0 || !strings.Contains(out, "dist/checksums.txt") {
		t.Errorf("the subjects step exited %d, output %q; want a failure naming dist/checksums.txt", code, out)
	}
}

// release is what the stub gh reports about the release the publish step
// publishes.
type release struct {
	draft      string            // what gh release view answers for isDraft
	published  string            // the published releases' tags, space-separated
	assets     map[string][]byte // what gh release download writes
	unverified string            // the digest gh attestation verify fails on
}

// publishRun is one run of the publish step: its exit code and output, the
// gh calls it made, in order, and the digests of the files it verified.
type publishRun struct {
	code     int
	out      string
	calls    []string
	verified []string
}

// lines splits a file into its lines, none when it does not exist.
func lines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// runPublish runs the publish job's publish step for the tag v0.1.2 with
// subjects downloaded and gh answering as r says.
func runPublish(t *testing.T, subjects string, r release) publishRun {
	t.Helper()
	temp := t.TempDir()
	writeFile(t, filepath.Join(temp, "subjects", "subjects.txt"), []byte(subjects))
	stub := t.TempDir()
	writeFile(t, filepath.Join(stub, "bin", "gh"), []byte(stubGH))
	err := os.Chmod(filepath.Join(stub, "bin", "gh"), 0o700) //nolint:gosec // the stub must be executable
	if err != nil {
		t.Fatalf("make the stub executable: %v", err)
	}
	assets := filepath.Join(stub, "assets")
	err = os.MkdirAll(assets, 0o750)
	if err != nil {
		t.Fatalf("create the assets folder: %v", err)
	}
	for name, data := range r.assets {
		writeFile(t, filepath.Join(assets, name), data)
	}
	log := filepath.Join(stub, "calls")
	verified := filepath.Join(stub, "verified")
	code, out := runStep(t, t.TempDir(), step(t, "publish", "publish"),
		"RUNNER_TEMP="+temp,
		"TAG=v0.1.2",
		"GITHUB_REPOSITORY="+repository,
		"PATH="+filepath.Join(stub, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GH_LOG="+log,
		"GH_VERIFIED="+verified,
		"STUB_DRAFT="+r.draft,
		"STUB_PUBLISHED="+r.published,
		"STUB_ASSETS="+assets,
		"STUB_UNVERIFIED="+r.unverified,
	)

	return publishRun{code: code, out: out, calls: lines(t, log), verified: lines(t, verified)}
}

// fakeRelease is a draft v0.1.2 whose assets match its subjects, after
// v0.1.1 shipped. It returns the subjects file and the release.
func fakeRelease(t *testing.T) (string, release) {
	t.Helper()
	archives := releaseArchives(t)

	return subjectsFile(archives), release{draft: "true", published: "v0.1.1", assets: archives}
}

// called reports whether any call starts with prefix.
func called(calls []string, prefix string) bool {
	return slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

// assertNotPublished fails unless the step failed, saying why, without
// editing the release.
func assertNotPublished(t *testing.T, got publishRun, why string) {
	t.Helper()
	if got.code == 0 || !strings.Contains(got.out, why) {
		t.Errorf("the publish step exited %d, output %q; want a failure saying %q", got.code, got.out, why)
	}
	if called(got.calls, "release edit") {
		t.Errorf("calls %q; want no release edit", got.calls)
	}
}

func TestPublishVerifiesEachArchiveAndItsCrewThenPublishes(t *testing.T) {
	subjects, r := fakeRelease(t)
	got := runPublish(t, subjects, r)
	if got.code != 0 {
		t.Fatalf("the publish step exited %d, output %q; want 0", got.code, got.out)
	}
	var verifies []string
	for _, c := range got.calls {
		if strings.HasPrefix(c, "attestation verify ") {
			verifies = append(verifies, c)
			if !strings.HasSuffix(c, " "+verifyFlags) {
				t.Errorf("call %q; want the flags %q", c, verifyFlags)
			}
		}
	}
	if len(verifies) != 2*len(builds) {
		t.Errorf("%d attestation verify calls %q; want %d", len(verifies), verifies, 2*len(builds))
	}
	want := make([]string, 0, 2*len(builds))
	for _, b := range builds {
		want = append(want, sum(r.assets[b.archive]), sum(b.binary))
	}
	slices.Sort(want)
	verified := slices.Sorted(slices.Values(got.verified))
	if !slices.Equal(verified, want) {
		t.Errorf("verified the digests %q; want each archive and each crew, %q", verified, want)
	}
	if len(got.calls) == 0 || got.calls[len(got.calls)-1] != "release edit v0.1.2 --draft=false --latest" {
		t.Errorf("calls %q; want release edit v0.1.2 --draft=false --latest last", got.calls)
	}
}

func TestPublishRefusesAReleaseThatIsNotADraft(t *testing.T) {
	subjects, r := fakeRelease(t)
	r.draft = "false"
	got := runPublish(t, subjects, r)
	assertNotPublished(t, got, "The release v0.1.2 is not a draft")
	if called(got.calls, "attestation verify") {
		t.Errorf("calls %q; want no attestation verify", got.calls)
	}
}

func TestPublishRefusesADraftBelowAPublishedRelease(t *testing.T) {
	subjects, r := fakeRelease(t)
	r.published = "v0.1.3 v0.1.1"
	got := runPublish(t, subjects, r)
	assertNotPublished(t, got, "v0.1.2 is not above the published release v0.1.3")
	if called(got.calls, "attestation verify") {
		t.Errorf("calls %q; want no attestation verify", got.calls)
	}
}

func TestPublishRefusesAnArchiveThatDiffersFromItsSubject(t *testing.T) {
	subjects, r := fakeRelease(t)
	r.assets["crew_linux_arm64.tar.gz"] = archive(t, []byte("another crew"))
	assertNotPublished(t, runPublish(t, subjects, r), "crew_linux_arm64.tar.gz is not an archive this run attested")
}

func TestPublishRefusesACrewWhoseAttestationFails(t *testing.T) {
	subjects, r := fakeRelease(t)
	r.unverified = sum(builds[2].binary)
	got := runPublish(t, subjects, r)
	assertNotPublished(t, got, "verification failed for "+r.unverified)
	if len(got.verified) == 0 || got.verified[len(got.verified)-1] != r.unverified {
		t.Errorf("verified %q; want the step to stop at the crew whose attestation fails, %s", got.verified, r.unverified)
	}
}

func TestPublishRefusesADownloadWithNoArchive(t *testing.T) {
	subjects, r := fakeRelease(t)
	r.assets = nil
	assertNotPublished(t, runPublish(t, subjects, r), "Verified 0 files; want 8")
}

func TestPublishRefusesAPartialDownload(t *testing.T) {
	subjects, r := fakeRelease(t)
	delete(r.assets, "crew_darwin_arm64.tar.gz")
	assertNotPublished(t, runPublish(t, subjects, r), "Verified 6 files; want 8")
}

func TestPublishRefusesAnArchiveMissingFromTheSubjects(t *testing.T) {
	subjects, r := fakeRelease(t)
	r.assets["crew_windows_amd64.tar.gz"] = archive(t, []byte("crew for windows amd64"))
	assertNotPublished(t, runPublish(t, subjects, r), "crew_windows_amd64.tar.gz is not an archive this run attested")
}

func TestPublishRefusesATagAlreadyPublished(t *testing.T) {
	subjects, r := fakeRelease(t)
	r.published = "v0.1.2 v0.1.1"
	got := runPublish(t, subjects, r)
	assertNotPublished(t, got, "v0.1.2 is not above the published release v0.1.2")
	if called(got.calls, "attestation verify") {
		t.Errorf("calls %q; want no attestation verify", got.calls)
	}
}

func TestPublishRefusesAnArchiveWhoseAttestationFails(t *testing.T) {
	subjects, r := fakeRelease(t)
	r.unverified = sum(r.assets[builds[1].archive])
	got := runPublish(t, subjects, r)
	assertNotPublished(t, got, "verification failed for "+r.unverified)
	if len(got.verified) == 0 || got.verified[len(got.verified)-1] != r.unverified {
		t.Errorf("verified %q; want the step to stop at the archive whose attestation fails, %s", got.verified, r.unverified)
	}
}

func TestPublishRefusesAnArchiveWithoutCrew(t *testing.T) {
	archives := releaseArchives(t)
	archives["crew_linux_amd64.tar.gz"] = pack(t, file{"LICENSE", []byte("MIT")}, file{"README.md", []byte("# crew")})
	r := release{draft: "true", published: "v0.1.1", assets: archives}
	got := runPublish(t, subjectsFile(archives), r)
	assertNotPublished(t, got, "crew: Not found in archive")
	if len(got.verified) == 0 || got.verified[len(got.verified)-1] != sum(archives["crew_linux_amd64.tar.gz"]) {
		t.Errorf("verified %q; want the step to stop after the archive without crew", got.verified)
	}
}

func TestOnlyAttestCanMintAnOIDCToken(t *testing.T) {
	w := readWorkflow(t)
	if w.Permissions == nil || len(w.Permissions) != 0 {
		t.Errorf("workflow permissions %v; want {}", w.Permissions)
	}
	if len(w.Jobs) == 0 {
		t.Fatal("the workflow has no job")
	}
	for name, j := range w.Jobs {
		token := j.Permissions["id-token"]
		if name == "attest" && token != "write" {
			t.Errorf("attest has id-token %q; want write", token)
		}
		if name != "attest" && token != "" {
			t.Errorf("%s has id-token %q; want none", name, token)
		}
	}
}

func TestJobsRunBuildThenAttestThenPublish(t *testing.T) {
	jobs := readWorkflow(t).Jobs
	want := map[string][]string{"build": nil, "attest": {"build"}, "publish": {"build", "attest"}}
	if len(jobs) != len(want) {
		t.Errorf("%d jobs; want build, attest and publish", len(jobs))
	}
	for name, needs := range want {
		j, ok := jobs[name]
		if !ok {
			t.Errorf("the workflow has no job %q", name)

			continue
		}
		if !slices.Equal(j.Needs, needs) {
			t.Errorf("%s needs %q; want %q", name, j.Needs, needs)
		}
	}
}
