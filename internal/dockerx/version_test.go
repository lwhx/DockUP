package dockerx

import "testing"

func TestSameEffectiveImageSameID(t *testing.T) {
	oldV := ImageVersion{ID: "sha256:abc123", Tag: "1.0.0"}
	newV := ImageVersion{ID: "abc123", Tag: "main"}
	if !SameEffectiveImage(oldV, newV) {
		t.Fatal("same image id should be treated as same effective image")
	}
}

func TestSameEffectiveImageSuppressesSameRevisionFloatingBuild(t *testing.T) {
	oldV := ImageVersion{ID: "sha256:5087656bca6d", Tag: "0.26.0", Revision: "7a352dc227bd30f3f6a223e9c72c5c8799affe0d"}
	newV := ImageVersion{ID: "sha256:917f949e28af", Revision: "7a352dc227bd30f3f6a223e9c72c5c8799affe0d"}
	if !SameEffectiveImage(oldV, newV) {
		t.Fatal("same source revision with one unversioned/floating build should not be reported as an update")
	}
}

func TestSameEffectiveImageKeepsDifferentRevisionUpdate(t *testing.T) {
	oldV := ImageVersion{ID: "sha256:5087656bca6d", Tag: "0.26.0", Revision: "old"}
	newV := ImageVersion{ID: "sha256:917f949e28af", Revision: "new"}
	if SameEffectiveImage(oldV, newV) {
		t.Fatal("different source revisions should be reported as updates")
	}
}

func TestSameEffectiveImageSuppressesFloatingRefWithResolvedReleaseTag(t *testing.T) {
	oldV := ImageVersion{ID: "sha256:5087656bca6d", Tag: "0.26.0", Revision: "7a352dc227bd30f3f6a223e9c72c5c8799affe0d"}
	newV := ImageVersion{ID: "sha256:917f949e28af", RemoteTag: "0.26.0", Ref: "ghcr.io/danger-dream/parrot:latest", Revision: "7a352dc227bd30f3f6a223e9c72c5c8799affe0d"}
	if !SameEffectiveImage(oldV, newV) {
		t.Fatal("floating latest builds resolved to the same release tag and source revision should not be reported as updates")
	}
}

func TestSameEffectiveImageKeepsPinnedSemverDigestChange(t *testing.T) {
	oldV := ImageVersion{ID: "sha256:old", Tag: "1.2.3", Ref: "repo/app:1.2.3", Revision: "same"}
	newV := ImageVersion{ID: "sha256:new", Tag: "1.2.3", Ref: "repo/app:1.2.3", Revision: "same"}
	if SameEffectiveImage(oldV, newV) {
		t.Fatal("pinned semver rebuilds should still be reported as Docker image updates")
	}
}
