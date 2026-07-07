package k8s

import (
	"context"
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

const testNS = "ctf-instances"

// newFakeClient builds a Client backed by a fake dynamic client, seeded with
// objects, and starts (and waits for) the Challenge informer so reads behave
// like production. The fake client's watch already carries all seed objects
// at creation, so the initial sync completes immediately.
func newFakeClient(t *testing.T, objects ...runtime.Object) *Client {
	t.Helper()
	scheme := runtime.NewScheme()
	gvrToKind := map[schema.GroupVersionResource]string{
		challengeGVR: "ChallengeList",
		instanceGVR:  "ChallengeInstanceList",
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToKind, objects...)
	c := NewWithDynamic(dyn, testNS)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.StartInformer(ctx); err != nil {
		t.Fatalf("StartInformer: %v", err)
	}
	return c
}

func staticChallengeObj(name, flag string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "ctf.ctf.io/v1alpha1",
		"kind":       "Challenge",
		"metadata":   map[string]any{"name": name, "namespace": testNS},
		"spec": map[string]any{
			"mode":        "static",
			"description": "hidden in the image",
			"scoring": map[string]any{
				"name": "Stego One", "category": "stegano", "value": int64(50), "state": "visible",
			},
			"static": map[string]any{
				"attachments": []any{
					map[string]any{"name": "img.png", "ociRef": "example.com/x:1", "sha256": "abc"},
				},
			},
		},
		"status": map[string]any{"staticFlag": flag},
	}}
}

func dynamicChallengeObj(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "ctf.ctf.io/v1alpha1",
		"kind":       "Challenge",
		"metadata":   map[string]any{"name": name, "namespace": testNS},
		"spec": map[string]any{
			"mode":    "dynamic",
			"timeout": int64(600),
			"scoring": map[string]any{
				"category": "web", "state": "hidden",
				"initial": int64(500), "decay": int64(15), "minimum": int64(75), "function": "linear",
			},
			"scenario": map[string]any{"image": "nginx:alpine", "port": int64(80)},
		},
	}}
}

func TestListChallengesProjectsFields(t *testing.T) {
	c := newFakeClient(t, staticChallengeObj("stego", "CTF{s}"), dynamicChallengeObj("web"))

	challenges, err := c.ListChallenges(context.Background())
	if err != nil {
		t.Fatalf("ListChallenges: %v", err)
	}
	if len(challenges) != 2 {
		t.Fatalf("got %d challenges, want 2", len(challenges))
	}
	if got := c.CacheSize(); got != 2 {
		t.Errorf("CacheSize() = %d, want 2", got)
	}

	byName := map[string]Challenge{}
	for _, ch := range challenges {
		byName[ch.Name] = ch
	}

	stego := byName["stego"]
	if stego.Mode != "static" || stego.StaticFlag != "CTF{s}" || stego.Category != "stegano" {
		t.Errorf("stego projection wrong: %+v", stego)
	}
	if stego.DisplayName != "Stego One" || stego.Value != 50 {
		t.Errorf("stego scoring wrong: %+v", stego)
	}
	if len(stego.Attachments) != 1 || stego.Attachments[0].Name != "img.png" {
		t.Errorf("stego attachments wrong: %+v", stego.Attachments)
	}

	web := byName["web"]
	if web.Mode != "dynamic" || web.State != "hidden" || web.Timeout != 600 {
		t.Errorf("web projection wrong: %+v", web)
	}
	if web.Initial != 500 || web.Decay != 15 || web.Minimum != 75 {
		t.Errorf("web scoring wrong: %+v", web)
	}
	// display name defaults to metadata.name when scoring.name is empty
	if web.DisplayName != "web" {
		t.Errorf("web display name = %q, want web", web.DisplayName)
	}
}

func TestGetChallengeNotFound(t *testing.T) {
	c := newFakeClient(t)
	if _, err := c.GetChallenge(context.Background(), "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestChallengeReadsFailExplicitlyBeforeSync exercises the case where the
// informer has not been started at all: reads must fail loudly rather than
// silently hitting the API server per request.
func TestChallengeReadsFailExplicitlyBeforeSync(t *testing.T) {
	scheme := runtime.NewScheme()
	gvrToKind := map[schema.GroupVersionResource]string{
		challengeGVR: "ChallengeList",
		instanceGVR:  "ChallengeInstanceList",
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToKind)
	c := NewWithDynamic(dyn, testNS)

	if _, err := c.ListChallenges(context.Background()); !errors.Is(err, ErrCacheNotSynced) {
		t.Errorf("ListChallenges err = %v, want ErrCacheNotSynced", err)
	}
	if _, err := c.GetChallenge(context.Background(), "stego"); !errors.Is(err, ErrCacheNotSynced) {
		t.Errorf("GetChallenge err = %v, want ErrCacheNotSynced", err)
	}
	if got := c.CacheSize(); got != 0 {
		t.Errorf("CacheSize() = %d, want 0 before StartInformer", got)
	}
}

func TestInstanceLifecycle(t *testing.T) {
	c := newFakeClient(t)
	ctx := context.Background()

	inst, err := c.CreateInstance(ctx, "web-abc123", "web", "team-1", 600)
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if inst.Name != "web-abc123" || inst.ChallengeName != "web" {
		t.Errorf("created instance wrong: %+v", inst)
	}
	if inst.Until == nil {
		t.Error("instance has no expiry")
	}

	got, err := c.GetInstance(ctx, "web-abc123")
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if got.SourceID != "team-1" {
		t.Errorf("sourceID = %q, want team-1", got.SourceID)
	}

	// Idempotent create returns the existing instance.
	again, err := c.CreateInstance(ctx, "web-abc123", "web", "team-1", 600)
	if err != nil {
		t.Fatalf("second CreateInstance: %v", err)
	}
	if again.Name != "web-abc123" {
		t.Errorf("re-create returned %q", again.Name)
	}

	if err := c.DeleteInstance(ctx, "web-abc123"); err != nil {
		t.Fatalf("DeleteInstance: %v", err)
	}
	if _, err := c.GetInstance(ctx, "web-abc123"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: err = %v, want ErrNotFound", err)
	}
	// Deleting a missing instance is a no-op.
	if err := c.DeleteInstance(ctx, "web-abc123"); err != nil {
		t.Errorf("delete missing instance: %v", err)
	}
}
