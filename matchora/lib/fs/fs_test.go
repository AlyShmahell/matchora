package fs

import "testing"

func TestWithinFilesystemRoot(t *testing.T) {
	if !Within("/", "/mnt/microsd/media/anime") {
		t.Fatal("absolute path under /")
	}
	if !Within("/", "/") {
		t.Fatal("root is inside itself")
	}
	if Within("/", "relative") {
		t.Fatal("relative path is not inside filesystem root")
	}
}

func TestWithinPrefix(t *testing.T) {
	if !Within("/media", "/media") {
		t.Fatal("equal")
	}
	if !Within("/media", "/media/tv") {
		t.Fatal("child")
	}
	if Within("/media", "/mnt") {
		t.Fatal("sibling")
	}
	if Within("/media", "/media2") {
		t.Fatal("prefix-not-separator")
	}
}
