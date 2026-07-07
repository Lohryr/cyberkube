// Package k8s reads Challenge CRs and manages ChallengeInstance CRs through
// the Kubernetes API. It uses the dynamic client with small projection
// structs: cyberkube only needs a handful of fields, and this keeps the
// module decoupled from the chall-operator codebase.
//
// Challenge reads are served from an informer cache (watch-based, refreshed
// by events) rather than the API server directly: at scale, listing
// challenges on every player request would turn the kube-apiserver into a
// bottleneck. Writes (ChallengeInstance CRUD) still go straight to the
// dynamic client — they are comparatively rare and must be immediately
// consistent.
package k8s

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	challengeGVR = schema.GroupVersionResource{Group: "ctf.ctf.io", Version: "v1alpha1", Resource: "challenges"}
	instanceGVR  = schema.GroupVersionResource{Group: "ctf.ctf.io", Version: "v1alpha1", Resource: "challengeinstances"}
)

// ErrNotFound is returned when a resource does not exist.
var ErrNotFound = fmt.Errorf("resource not found")

// ErrCacheNotSynced is returned by Challenge reads when the informer cache
// has not (yet, or anymore) completed its initial sync. Callers should treat
// this as a transient 5xx rather than silently falling back to a live API
// call, which is exactly the per-request load this cache exists to avoid.
var ErrCacheNotSynced = fmt.Errorf("challenge cache not synced")

// defaultResync is how often the informer relists challenges from the API
// server as a safety net on top of watch events.
const defaultResync = 5 * time.Minute

// Challenge is the projection of a Challenge CR that cyberkube consumes.
type Challenge struct {
	Name        string
	Mode        string // static | dynamic
	Description string

	// scoring
	DisplayName   string
	Category      string
	Value         int
	State         string // visible | hidden
	Shared        bool
	DestroyOnFlag bool
	Initial       int
	Decay         int
	Minimum       int
	Function      string

	// static
	StaticFlag  string // decrypted, from status (operator-populated)
	Attachments []Attachment

	// dynamic
	Timeout int64
}

// Attachment references a static challenge file.
type Attachment struct {
	Name   string
	OCIRef string
	SHA256 string
}

// Instance is the projection of a ChallengeInstance CR.
type Instance struct {
	Name           string
	ChallengeName  string
	SourceID       string
	Phase          string
	Ready          bool
	ConnectionInfo string
	Flags          []string
	Until          *time.Time
}

// Client wraps the dynamic Kubernetes client for writes, and an informer
// cache for Challenge reads.
type Client struct {
	dyn       dynamic.Interface
	namespace string

	challengeLister cache.GenericLister
	challengeSynced cache.InformerSynced
}

// New builds a client from in-cluster config, falling back to the given
// kubeconfig path (dev).
func New(kubeconfigPath, namespace string) (*Client, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("kubernetes config: %w", err)
		}
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	return &Client{dyn: dyn, namespace: namespace}, nil
}

// NewWithDynamic builds a client from an existing dynamic interface (tests).
func NewWithDynamic(dyn dynamic.Interface, namespace string) *Client {
	return &Client{dyn: dyn, namespace: namespace}
}

// StartInformer starts the Challenge CR informer (watch + cache) scoped to
// the client's namespace and blocks until its cache has completed the
// initial sync, syncTimeout elapses, or ctx is done. ctx must be the
// process-lifetime context: it also drives the watch goroutines, so a
// short-lived ctx would freeze the cache at the initial sync once it
// expires. It must be called once before ListChallenges or GetChallenge
// serve real traffic; calling it more than once is not supported.
func (c *Client) StartInformer(ctx context.Context, syncTimeout time.Duration) error {
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		c.dyn, defaultResync, c.namespace, nil)
	informer := factory.ForResource(challengeGVR)
	c.challengeLister = informer.Lister()
	c.challengeSynced = informer.Informer().HasSynced

	factory.Start(ctx.Done())
	waitCtx, cancelWait := context.WithTimeout(ctx, syncTimeout)
	defer cancelWait()
	synced := factory.WaitForCacheSync(waitCtx.Done())
	for gvr, ok := range synced {
		if !ok {
			return fmt.Errorf("wait for cache sync of %s: %w", gvr, waitCtx.Err())
		}
	}
	return nil
}

// CacheSize returns the number of Challenge CRs currently held in the
// informer cache, or 0 if the informer has not been started.
func (c *Client) CacheSize() int {
	if c.challengeLister == nil {
		return 0
	}
	objs, err := c.challengeLister.ByNamespace(c.namespace).List(labels.Everything())
	if err != nil {
		return 0
	}
	return len(objs)
}

// ListChallenges returns all Challenge CRs in the namespace from the
// informer cache. Returns ErrCacheNotSynced if StartInformer has not
// completed.
func (c *Client) ListChallenges(_ context.Context) ([]Challenge, error) {
	if c.challengeLister == nil || !c.challengeSynced() {
		return nil, ErrCacheNotSynced
	}
	objs, err := c.challengeLister.ByNamespace(c.namespace).List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list challenges: %w", err)
	}
	challenges := make([]Challenge, 0, len(objs))
	for _, obj := range objs {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		challenges = append(challenges, projectChallenge(u))
	}
	return challenges, nil
}

// GetChallenge returns one Challenge CR by name from the informer cache.
// Returns ErrCacheNotSynced if StartInformer has not completed.
func (c *Client) GetChallenge(_ context.Context, name string) (*Challenge, error) {
	if c.challengeLister == nil || !c.challengeSynced() {
		return nil, ErrCacheNotSynced
	}
	obj, err := c.challengeLister.ByNamespace(c.namespace).Get(name)
	if apierrors.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get challenge %s: %w", name, err)
	}
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("get challenge %s: unexpected cached object type %T", name, obj)
	}
	ch := projectChallenge(u)
	return &ch, nil
}

func projectChallenge(obj *unstructured.Unstructured) Challenge {
	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	ch := Challenge{
		Name:        obj.GetName(),
		Mode:        nestedString(spec, "mode"),
		Description: nestedString(spec, "description"),
		Timeout:     nestedInt64(spec, "timeout"),
	}
	if ch.Mode == "" {
		ch.Mode = "dynamic"
	}

	if scoring, ok, _ := unstructured.NestedMap(obj.Object, "spec", "scoring"); ok {
		ch.DisplayName = nestedString(scoring, "name")
		ch.Category = nestedString(scoring, "category")
		ch.Value = int(nestedInt64(scoring, "value"))
		ch.State = nestedString(scoring, "state")
		ch.Shared = nestedBool(scoring, "shared")
		ch.DestroyOnFlag = nestedBool(scoring, "destroyOnFlag")
		ch.Initial = int(nestedInt64(scoring, "initial"))
		ch.Decay = int(nestedInt64(scoring, "decay"))
		ch.Minimum = int(nestedInt64(scoring, "minimum"))
		ch.Function = nestedString(scoring, "function")
	}
	if ch.DisplayName == "" {
		ch.DisplayName = ch.Name
	}
	if ch.State == "" {
		ch.State = "visible"
	}

	ch.StaticFlag, _, _ = unstructured.NestedString(obj.Object, "status", "staticFlag")

	if atts, ok, _ := unstructured.NestedSlice(obj.Object, "spec", "static", "attachments"); ok {
		for _, a := range atts {
			m, ok := a.(map[string]any)
			if !ok {
				continue
			}
			ch.Attachments = append(ch.Attachments, Attachment{
				Name:   nestedString(m, "name"),
				OCIRef: nestedString(m, "ociRef"),
				SHA256: nestedString(m, "sha256"),
			})
		}
	}
	return ch
}

// GetInstance returns a ChallengeInstance by name.
func (c *Client) GetInstance(ctx context.Context, name string) (*Instance, error) {
	obj, err := c.dyn.Resource(instanceGVR).Namespace(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get instance %s: %w", name, err)
	}
	inst := projectInstance(obj)
	return &inst, nil
}

// CreateInstance creates a ChallengeInstance for the given challenge and
// source (team or user).
func (c *Client) CreateInstance(ctx context.Context, name, challengeName, sourceID string, timeout int64) (*Instance, error) {
	now := time.Now().UTC()
	until := now.Add(time.Duration(timeout) * time.Second)
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "ctf.ctf.io/v1alpha1",
		"kind":       "ChallengeInstance",
		"metadata": map[string]any{
			"name":      name,
			"namespace": c.namespace,
			"labels": map[string]any{
				"ctf.io/challenge": challengeName,
				"ctf.io/source":    sourceID,
			},
		},
		"spec": map[string]any{
			"challengeId":   challengeName,
			"sourceId":      sourceID,
			"challengeName": challengeName,
			"since":         now.Format(time.RFC3339),
			"until":         until.Format(time.RFC3339),
		},
	}}
	created, err := c.dyn.Resource(instanceGVR).Namespace(c.namespace).Create(ctx, obj, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return c.GetInstance(ctx, name)
	}
	if err != nil {
		return nil, fmt.Errorf("create instance %s: %w", name, err)
	}
	inst := projectInstance(created)
	return &inst, nil
}

// DeleteInstance removes a ChallengeInstance.
func (c *Client) DeleteInstance(ctx context.Context, name string) error {
	err := c.dyn.Resource(instanceGVR).Namespace(c.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete instance %s: %w", name, err)
	}
	return nil
}

// MarkInstanceSolved sets status.flagValidated so the operator janitor
// deletes the instance (destroyOnFlag behavior).
func (c *Client) MarkInstanceSolved(ctx context.Context, name string) error {
	patch := []byte(`{"status":{"flagValidated":true}}`)
	_, err := c.dyn.Resource(instanceGVR).Namespace(c.namespace).
		Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}, "status")
	if err != nil {
		return fmt.Errorf("mark instance %s solved: %w", name, err)
	}
	return nil
}

func projectInstance(obj *unstructured.Unstructured) Instance {
	inst := Instance{Name: obj.GetName()}
	inst.ChallengeName, _, _ = unstructured.NestedString(obj.Object, "spec", "challengeName")
	inst.SourceID, _, _ = unstructured.NestedString(obj.Object, "spec", "sourceId")
	inst.Phase, _, _ = unstructured.NestedString(obj.Object, "status", "phase")
	inst.Ready, _, _ = unstructured.NestedBool(obj.Object, "status", "ready")
	inst.ConnectionInfo, _, _ = unstructured.NestedString(obj.Object, "status", "connectionInfo")
	if flags, ok, _ := unstructured.NestedStringSlice(obj.Object, "status", "flags"); ok {
		inst.Flags = flags
	}
	if untilStr, ok, _ := unstructured.NestedString(obj.Object, "spec", "until"); ok && untilStr != "" {
		if t, err := time.Parse(time.RFC3339, untilStr); err == nil {
			inst.Until = &t
		}
	}
	return inst
}

func nestedString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func nestedInt64(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

func nestedBool(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}
