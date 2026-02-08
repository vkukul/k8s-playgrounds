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
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	AnnotationExpiresAt  = "secret-operator.example.com/expires-at"
	AnnotationWarnBefore = "secret-operator.example.com/warn-before"
)

type SecretController struct {
	clientset kubernetes.Interface
	informer  cache.SharedIndexInformer
	workqueue workqueue.TypedRateLimitingInterface[string]
	stopCh    chan struct{}
}

type SecretInfo struct {
	Name         string
	Namespace    string
	ExpiresAt    time.Time
	WarnBefore   time.Duration
	DaysUntilExp int
}

// NewSecretController creates a controller with an informer, workqueue, and event handlers
func NewSecretController(clientset kubernetes.Interface) *SecretController {
	informerFactory := informers.NewSharedInformerFactory(clientset, 30*time.Second)
	secretInformer := informerFactory.Core().V1().Secrets().Informer()

	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)

	controller := &SecretController{
		clientset: clientset,
		informer:  secretInformer,
		workqueue: queue,
		stopCh:    make(chan struct{}),
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

	fmt.Println("\nStarting Secret Rotation Controller")
	fmt.Println("====================================")

	fmt.Println("Starting informer...")
	go c.informer.Run(c.stopCh)

	fmt.Println("Waiting for informer cache to sync...")
	if !cache.WaitForCacheSync(c.stopCh, c.informer.HasSynced) {
		return fmt.Errorf("failed to sync informer cache")
	}
	fmt.Println("✓ Cache synced successfully")

	fmt.Printf("\nStarting %d worker(s)...\n", workers)
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, c.stopCh)
	}

	fmt.Println("✓ Workers started")
	fmt.Println("\nController is now watching for Secret changes...")
	fmt.Println("Try: kubectl apply -f scripts/test-secrets.yaml")
	fmt.Println("Or:  kubectl edit secret <secret-name>")
	fmt.Println("\nPress Ctrl+C to stop")

	<-c.stopCh
	fmt.Println("Stopping workers...")

	return nil
}

// Stop gracefully shuts down the controller
func (c *SecretController) Stop() {
	fmt.Println("\n\nShutting down controller...")
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
		fmt.Printf("Error reconciling %s (will retry): %v\n", key, err)
		return true
	}

	c.workqueue.Forget(key)
	return true
}

// reconcile fetches a Secret from the cache and checks its expiration status
func (c *SecretController) reconcile(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		fmt.Printf("Invalid key format: %s\n", key)
		return nil
	}

	obj, exists, err := c.informer.GetStore().GetByKey(key)
	if err != nil {
		return fmt.Errorf("error fetching object %s from cache: %w", key, err)
	}

	if !exists {
		fmt.Printf("Secret %s no longer exists\n", key)
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

	fmt.Printf("Reconciling Secret %s/%s\n", namespace, name)
	fmt.Printf("   Expires: %s (%d days)\n",
		info.ExpiresAt.Format("2006-01-02"),
		info.DaysUntilExp)

	analyzeSecret(info)

	return nil
}

// handleAdd enqueues newly created Secrets that have expiration annotations
func (c *SecretController) handleAdd(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		fmt.Printf("Error getting key for added object: %v\n", err)
		return
	}

	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return
	}

	if _, hasExpiration := parseSecretInfo(secret); !hasExpiration {
		return
	}

	fmt.Printf("ADD event: %s (queued for processing)\n", key)
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
		fmt.Printf("Error getting key for updated object: %v\n", err)
		return
	}

	_, oldHasExp := parseSecretInfo(oldSecret)
	_, newHasExp := parseSecretInfo(newSecret)

	if oldHasExp || newHasExp {
		fmt.Printf("UPDATE event: %s (queued for processing)\n", key)
		c.workqueue.Add(key)
	}
}

// handleDelete logs when a tracked Secret is removed from the cluster
func (c *SecretController) handleDelete(obj interface{}) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			fmt.Printf("Error: unexpected type in handleDelete: %T\n", obj)
			return
		}
		secret, ok = tombstone.Obj.(*corev1.Secret)
		if !ok {
			fmt.Printf("Error: tombstone contained unexpected type: %T\n", tombstone.Obj)
			return
		}
	}

	if _, hasExpiration := parseSecretInfo(secret); !hasExpiration {
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(secret)
	if err != nil {
		fmt.Printf("Error getting key for deleted object: %v\n", err)
		return
	}

	fmt.Printf("DELETE event: %s (was tracking expiration)\n", key)
}

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

// analyzeSecret checks the expiration status and prints appropriate warnings
func analyzeSecret(info SecretInfo) {
	warnThresholdDays := int(info.WarnBefore.Hours() / 24)

	if info.DaysUntilExp < 0 {
		fmt.Printf("   EXPIRED %d days ago - action required!\n", -info.DaysUntilExp)
	} else if info.DaysUntilExp <= warnThresholdDays {
		fmt.Printf("   EXPIRING SOON: %d days remaining (within %d day warning threshold)\n",
			info.DaysUntilExp, warnThresholdDays)
	} else {
		fmt.Printf("   OK: %d days until warning threshold\n",
			info.DaysUntilExp-warnThresholdDays)
	}
	fmt.Println()
}
