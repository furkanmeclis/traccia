package usecase

import "testing"

func TestJoinAndParent(t *testing.T) {
	t.Parallel()
	key, err := joinKey("Projects/", "Website")
	if err != nil {
		t.Fatal(err)
	}
	if key != "Projects/Website" {
		t.Fatalf("got %q", key)
	}
	if parentPrefix("Projects/Website/logo.png") != "Projects/Website/" {
		t.Fatalf("parent: %q", parentPrefix("Projects/Website/logo.png"))
	}
	if baseName("Projects/Website/logo.png") != "logo.png" {
		t.Fatalf("base: %q", baseName("Projects/Website/logo.png"))
	}
	if _, err := joinKey("a", "../x"); err == nil {
		t.Fatal("expected invalid name")
	}
}

func TestClassifyFileKind(t *testing.T) {
	t.Parallel()
	if classifyFileKind("a.pdf", "") != "pdf" {
		t.Fatal("pdf")
	}
	if classifyFileKind("a.png", "image/png") != "image" {
		t.Fatal("image")
	}
	if classifyFileKind("main.go", "") != "code" {
		t.Fatal("code")
	}
}

func TestAttachmentNamespaceIsNotAPlatformStorageGrant(t *testing.T) {
	for _, key := range []string{"__system/attachments", "__system/attachments/", "/__system/attachments/w/u/final/file", "  __system/attachments/w/u/staging/  "} {
		if _, e := normalizeKey(key); e != ErrInvalidKey {
			t.Fatalf("key %q escaped fence: %v", key, e)
		}
		if _, e := normalizePrefix(key); e != ErrInvalidKey {
			t.Fatalf("prefix %q escaped fence: %v", key, e)
		}
	}
	for _, key := range []string{"attachments/file", "__system/attachments-other/file", "__system/trash/file"} {
		if _, e := normalizeKey(key); e != nil {
			t.Fatalf("unrelated key changed: %q", key)
		}
	}
}
