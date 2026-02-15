package controller

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	AnnotationExpiresAt  = "secret-operator.example.com/expires-at"
	AnnotationWarnBefore = "secret-operator.example.com/warn-before"
)

type SecretController struct {
	clientset   kubernetes.Interface
	informer    cache.SharedIndexInformer
	workqueue   workqueue.TypedRateLimitingInterface[string]
	recorder    record.EventRecorder
	broadcaster record.EventBroadcaster
	stopCh      chan struct{}
}

type SecretInfo struct {
	Name         string
	Namespace    string
	ExpiresAt    time.Time
	WarnBefore   time.Duration
	DaysUntilExp int
}

// NewSecretController creates a controller with an informer, workqueue, event recorder, and event handlers
func NewSecretController(clientset kubernetes.Interface) *SecretController {
	informerFactory := informers.NewSharedInformerFactory(clientset, 30*time.Second)
	secretInformer := informerFactory.Core().V1().Secrets().Informer()

	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)

	// EventBroadcaster receives events and sends them to the API server
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: clientset.CoreV1().Events(""),
	})

	// EventRecorder creates events attached to specific Kubernetes objects
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{
		Component: "secret-operator",
	})

	controller := &SecretController{
		clientset:   clientset,
		informer:    secretInformer,
		workqueue:   queue,
		recorder:    recorder,
		broadcaster: broadcaster,
		stopCh:      make(chan struct{}),
	}

	secretInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.handleAdd,
		UpdateFunc: controller.handleUpdate,
		DeleteFunc: controller.handleDelete,
	})

	return controller
}

// Run starts the informer and launches the given number of workers to process the queue
func (c *SecretController) Run(workers int) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("Starting Secret Rotation Controller")

	klog.Info("Starting informer...")
	go c.informer.Run(c.stopCh)

	klog.Info("Waiting for informer cache to sync...")
	if !cache.WaitForCacheSync(c.stopCh, c.informer.HasSynced) {
		return fmt.Errorf("failed to sync informer cache")
	}
	klog.Info("Cache synced successfully")

	klog.Infof("Starting %d worker(s)...", workers)
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, c.stopCh)
	}

	klog.Info("Workers started. Watching for Secret changes...")
	fmt.Println("\nTry: kubectl apply -f scripts/test-secrets.yaml")
	fmt.Println("Or:  kubectl edit secret <secret-name>")
	fmt.Println("Press Ctrl+C to stop")

	<-c.stopCh
	klog.Info("Stopping workers...")

	return nil
}

// Stop gracefully shuts down the controller and event broadcaster
func (c *SecretController) Stop() {
	klog.Info("Shutting down controller...")
	c.broadcaster.Shutdown()
	close(c.stopCh)
}

// runWorker loops, processing items from the workqueue until shutdown
func (c *SecretController) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem pulls one key from the queue and reconciles it. Returns false on shutdown
func (c *SecretController) processNextWorkItem() bool {
	key, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}
	defer c.workqueue.Done(key)

	err := c.reconcile(key)
	if err != nil {
		c.workqueue.AddRateLimited(key)
		klog.Errorf("Error reconciling %s (will retry): %v", key, err)
		return true
	}

	c.workqueue.Forget(key)
	return true
}

// reconcile fetches a Secret from the cache, checks its expiration, and emits Kubernetes Events
func (c *SecretController) reconcile(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Errorf("Invalid key format: %s", key)
		return nil
	}

	obj, exists, err := c.informer.GetStore().GetByKey(key)
	if err != nil {
		return fmt.Errorf("error fetching object %s from cache: %w", key, err)
	}

	if !exists {
		klog.Infof("Secret %s no longer exists", key)
		return nil
	}

	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return fmt.Errorf("unexpected object type in cache: %T", obj)
	}

	info, hasExpiration := parseSecretInfo(secret)
	if !hasExpiration {
		return nil
	}

	klog.Infof("Reconciling %s/%s | expires: %s | days: %d",
		namespace, name,
		info.ExpiresAt.Format("2006-01-02"),
		info.DaysUntilExp)

	c.emitExpirationEvent(secret, info)

	return nil
}

// emitExpirationEvent creates a Kubernetes Event on the Secret based on its expiration status
func (c *SecretController) emitExpirationEvent(secret *corev1.Secret, info SecretInfo) {
	warnThresholdDays := int(info.WarnBefore.Hours() / 24)

	if info.DaysUntilExp < 0 {
		klog.Warningf("  EXPIRED %d days ago: %s/%s", -info.DaysUntilExp, info.Namespace, info.Name)
		c.recorder.Eventf(secret, corev1.EventTypeWarning, "SecretExpired",
			"Secret expired %d days ago (expired on %s)",
			-info.DaysUntilExp, info.ExpiresAt.Format("2006-01-02"))

	} else if info.DaysUntilExp <= warnThresholdDays {
		klog.Warningf("  EXPIRING SOON in %d days: %s/%s", info.DaysUntilExp, info.Namespace, info.Name)
		c.recorder.Eventf(secret, corev1.EventTypeWarning, "SecretExpiringSoon",
			"Secret expires in %d days (on %s). Warning threshold: %d days",
			info.DaysUntilExp, info.ExpiresAt.Format("2006-01-02"), warnThresholdDays)

	} else {
		klog.Infof("  OK: %d days remaining for %s/%s", info.DaysUntilExp, info.Namespace, info.Name)
		c.recorder.Eventf(secret, corev1.EventTypeNormal, "SecretValid",
			"Secret is valid. Expires in %d days (on %s)",
			info.DaysUntilExp, info.ExpiresAt.Format("2006-01-02"))
	}
}

// handleAdd enqueues newly created Secrets that have expiration annotations
func (c *SecretController) handleAdd(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Error getting key for added object: %v", err)
		return
	}

	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return
	}

	if _, hasExpiration := parseSecretInfo(secret); !hasExpiration {
		return
	}

	klog.V(2).Infof("ADD event: %s", key)
	c.workqueue.Add(key)
}

// handleUpdate enqueues Secrets when expiration annotations are added, changed, or removed
func (c *SecretController) handleUpdate(oldObj, newObj interface{}) {
	oldSecret, ok := oldObj.(*corev1.Secret)
	if !ok {
		return
	}

	newSecret, ok := newObj.(*corev1.Secret)
	if !ok {
		return
	}

	// Filter out resync events
	if oldSecret.ResourceVersion == newSecret.ResourceVersion {
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(newObj)
	if err != nil {
		klog.Errorf("Error getting key for updated object: %v", err)
		return
	}

	_, oldHasExp := parseSecretInfo(oldSecret)
	_, newHasExp := parseSecretInfo(newSecret)

	if oldHasExp || newHasExp {
		klog.V(2).Infof("UPDATE event: %s", key)
		c.workqueue.Add(key)
	}
}

// handleDelete logs when a tracked Secret is removed from the cluster
func (c *SecretController) handleDelete(obj interface{}) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Unexpected type in handleDelete: %T", obj)
			return
		}
		secret, ok = tombstone.Obj.(*corev1.Secret)
		if !ok {
			klog.Errorf("Tombstone contained unexpected type: %T", tombstone.Obj)
			return
		}
	}

	if _, hasExpiration := parseSecretInfo(secret); !hasExpiration {
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(secret)
	if err != nil {
		klog.Errorf("Error getting key for deleted object: %v", err)
		return
	}

	klog.Infof("DELETE: %s (was tracking expiration)", key)
}

// Helpers

// parseSecretInfo extracts expiration information from a Secret's annotations
func parseSecretInfo(secret *corev1.Secret) (SecretInfo, bool) {
	expiresAt, hasExpiration := secret.Annotations[AnnotationExpiresAt]
	if !hasExpiration {
		return SecretInfo{}, false
	}

	expTime, err := time.Parse("2006-01-02", expiresAt)
	if err != nil {
		return SecretInfo{}, false
	}

	warnBefore := 7 * 24 * time.Hour
	if warnStr, hasWarn := secret.Annotations[AnnotationWarnBefore]; hasWarn {
		if parsed, err := parseDuration(warnStr); err == nil {
			warnBefore = parsed
		}
	}

	now := time.Now()
	daysUntil := int(expTime.Sub(now).Hours() / 24)

	return SecretInfo{
		Name:         secret.Name,
		Namespace:    secret.Namespace,
		ExpiresAt:    expTime,
		WarnBefore:   warnBefore,
		DaysUntilExp: daysUntil,
	}, true
}

// parseDuration extends time.ParseDuration with support for "d" (days)
func parseDuration(s string) (time.Duration, error) {
	if len(s) > 1 && s[len(s)-1] == 'd' {
		daysStr := strings.TrimSuffix(s, "d")
		days, err := strconv.Atoi(daysStr)
		if err != nil {
			return 0, fmt.Errorf("invalid days format: %w", err)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
