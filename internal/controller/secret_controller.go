package controller

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Annotation keys we look for on Secrets
const (
	AnnotationExpiresAt  = "secret-operator.example.com/expires-at"
	AnnotationWarnBefore = "secret-operator.example.com/warn-before"
)

// SecretController watches Secrets and identifies those approaching expiration
type SecretController struct {
	clientset kubernetes.Interface
	informer  cache.SharedIndexInformer
	stopCh    chan struct{}
}

// SecretInfo holds information about a Secret with expiration metadata
type SecretInfo struct {
	Name         string
	Namespace    string
	ExpiresAt    time.Time
	WarnBefore   time.Duration
	DaysUntilExp int
}

// NewSecretController creates a new instance of the Secret controller
func NewSecretController(clientset kubernetes.Interface) *SecretController {
	informerFactory := informers.NewSharedInformerFactory(clientset, 30*time.Second)
	secretInformer := informerFactory.Core().V1().Secrets().Informer()

	controller := &SecretController{
		clientset: clientset,
		informer:  secretInformer,
		stopCh:    make(chan struct{}),
	}

	secretInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.handleAdd,
		UpdateFunc: controller.handleUpdate,
		DeleteFunc: controller.handleDelete,
	})

	return controller
}

// Run starts the controller and blocks until stopCh is closed
func (c *SecretController) Run() error {
	defer runtime.HandleCrash()

	fmt.Println("\nStarting Secret Rotation Controller...")
	fmt.Println("Watching for Secret changes across all namespaces...")

	go c.informer.Run(c.stopCh)

	fmt.Println("Waiting for informer cache to sync...")
	if !cache.WaitForCacheSync(c.stopCh, c.informer.HasSynced) {
		return fmt.Errorf("failed to sync informer cache")
	}

	fmt.Println("✓ Cache synced successfully")
	fmt.Println("\nNow watching for Secret changes...")
	fmt.Println("Try: kubectl apply -f scripts/test-secrets.yaml")
	fmt.Println("Or:  kubectl edit secret <secret-name>")
	fmt.Println("\nPress Ctrl+C to stop")

	<-c.stopCh
	return nil
}

// Stop gracefully shuts down the controller
func (c *SecretController) Stop() {
	fmt.Println("\n\nShutting down controller...")
	close(c.stopCh)
}

// handleAdd is called when a new Secret is created in the cluster
func (c *SecretController) handleAdd(obj interface{}) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		fmt.Printf("Error: unexpected type in handleAdd: %T\n", obj)
		return
	}

	info, hasExpiration := parseSecretInfo(secret)
	if !hasExpiration {
		return
	}

	fmt.Printf("➕ ADD: Secret %s/%s (expires: %s)\n",
		secret.Namespace,
		secret.Name,
		info.ExpiresAt.Format("2006-01-02"))

	analyzeSecret(info)
}

// handleUpdate is called when an existing Secret is modified
func (c *SecretController) handleUpdate(oldObj, newObj interface{}) {
	oldSecret, ok := oldObj.(*corev1.Secret)
	if !ok {
		fmt.Printf("Error: unexpected type in handleUpdate (old): %T\n", oldObj)
		return
	}

	newSecret, ok := newObj.(*corev1.Secret)
	if !ok {
		fmt.Printf("Error: unexpected type in handleUpdate (new): %T\n", newObj)
		return
	}

	// Parse both old and new versions
	oldInfo, hadExpiration := parseSecretInfo(oldSecret)
	newInfo, hasExpiration := parseSecretInfo(newSecret)

	// Case 1: Expiration annotation was removed
	if hadExpiration && !hasExpiration {
		fmt.Printf("UPDATE: Secret %s/%s - expiration tracking removed\n",
			newSecret.Namespace, newSecret.Name)
		return
	}

	// Case 2: Expiration annotation was added
	if !hadExpiration && hasExpiration {
		fmt.Printf("UPDATE: Secret %s/%s - expiration tracking added (expires: %s)\n",
			newSecret.Namespace,
			newSecret.Name,
			newInfo.ExpiresAt.Format("2006-01-02"))
		analyzeSecret(newInfo)
		return
	}

	// Case 3: Neither version has expiration - ignore
	if !hasExpiration {
		return
	}

	// Case 4: Expiration date changed
	if !oldInfo.ExpiresAt.Equal(newInfo.ExpiresAt) {
		fmt.Printf("UPDATE: Secret %s/%s - expiration changed (%s → %s)\n",
			newSecret.Namespace,
			newSecret.Name,
			oldInfo.ExpiresAt.Format("2006-01-02"),
			newInfo.ExpiresAt.Format("2006-01-02"))
		analyzeSecret(newInfo)
	}
}

// handleDelete is called when a Secret is deleted from the cluster
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

	// Check if this was a managed Secret
	_, hasExpiration := parseSecretInfo(secret)
	if !hasExpiration {
		return
	}

	fmt.Printf("DELETE: Secret %s/%s (was tracking expiration)\n",
		secret.Namespace, secret.Name)
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

// parseDuration parses duration strings like "7d", "30d", "24h"
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
