package engine

import "testing"

func TestScrubShortensRootAndHomeOnlyAtWholePathBoundaries(t *testing.T) {
	const root, home = "/home/jo/repo", "/home/jo"
	tests := []struct {
		name       string
		root, home string
		text, want string
	}{
		{"home", root, home, "see /home/jo/.gitconfig", "see ~/.gitconfig"},
		{"a longer name is another directory", root, home, "see /home/joe/.gitconfig", "see /home/joe/.gitconfig"},
		{"a path inside another is not home", root, home, "see /mnt/home/jo/x", "see /mnt/home/jo/x"},
		{"root inside home wins", root, home, "open /home/jo/repo/main.go", "open ./main.go"},
		{"empty home shortens nothing", "/srv/repo", "", "see /home/jo/x", "see /home/jo/x"},
		{"root at the end of the text", root, home, "cd /home/jo/repo", "cd ."},
		{"root followed by a slash", root, home, "in /home/jo/repo/", "in ./"},
		{"root ending a sentence", root, home, "failed in /home/jo/repo.", "failed in .."},
		{"root ending a sentence mid-text", root, home, "failed in /home/jo/repo. Retry.", "failed in .. Retry."},
		{"root ending a sentence in brackets", root, home, "(in /home/jo/repo.)", "(in ..)"},
		{"root trailed by an ellipsis", root, home, "working in /home/jo/repo...", "working in ...."},
		{"a dotted name is another path", root, home, "clone /home/jo/repo.git", "clone ~/repo.git"},
		{"home ending a sentence", root, home, "no config in /home/jo.", "no config in ~."},
		{"home ending a sentence before a space", root, home, "no config in /home/jo. Retry", "no config in ~. Retry"},
		{"a dotted name under home", root, home, "see /home/jo.bak", "see /home/jo.bak"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Engine{cfg: Config{Root: tt.root, Home: tt.home}}
			if got := e.scrub(tt.text); got != tt.want {
				t.Errorf("scrub(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestReplaceDirLeavesAnEmptyDirOrTheFilesystemRootAlone(t *testing.T) {
	for _, dir := range []string{"", "/"} {
		if got := replaceDir("see /home/jo", dir, "~"); got != "see /home/jo" {
			t.Errorf("replaceDir with dir %q = %q, want the text unchanged", dir, got)
		}
	}
}
