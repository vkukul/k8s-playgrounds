package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	fmt.Println("Secret Rotation Operator - Starting up...")
	fmt.Println("==========================================")

	config, err := buildConfig()
	if err != nil {
		fmt.Printf("Error building kubeconfig: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Loaded kubeconfig successfully")

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("Error creating clientset: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Created Kubernetes clientset")

	fmt.Println("\nVerifying cluster connection...")
	if err := verifyConnection(clientset); err != nil {
		fmt.Printf("Error connecting to cluster: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n==========================================")
	fmt.Println("Phase 2: Reading and Analyzing Secrets")
	fmt.Println("==========================================")

	if err := listAndAnalyzeSecrets(clientset); err != nil {
		fmt.Printf("Error analyzing secrets: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n==========================================")
	fmt.Println("Secret Rotation Operator - Ready!")
	fmt.Println("(Controller logic will be added in Phase 3)")
}

func buildConfig() (*rest.Config, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot find home directory: %w", err)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	fmt.Printf("Using kubeconfig: %s\n", kubeconfig)
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

func verifyConnection(clientset *kubernetes.Clientset) error {
	ctx := context.TODO()

	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list namespaces: %w", err)
	}

	fmt.Printf("✓ Connected to cluster! Found %d namespaces:\n", len(namespaces.Items))

	for _, ns := range namespaces.Items {
		fmt.Printf("  - %s (status: %s)\n", ns.Name, ns.Status.Phase)
	}

	return nil
}

const (
	AnnotationExpiresAt  = "secret-operator.example.com/expires-at"
	AnnotationWarnBefore = "secret-operator.example.com/warn-before"
)

type SecretInfo struct {
	Name         string
	Namespace    string
	ExpiresAt    time.Time
	WarnBefore   time.Duration
	DaysUntilExp int
}

func listAndAnalyzeSecrets(clientset *kubernetes.Clientset) error {
	ctx := context.TODO()

	fmt.Println("\nListing Secrets across all namespaces...")
	secrets, err := clientset.CoreV1().Secrets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list secrets: %w", err)
	}

	fmt.Printf("Found %d total secrets in the cluster\n\n", len(secrets.Items))

	var managedSecrets []SecretInfo
	for _, secret := range secrets.Items {
		if info, ok := parseSecretInfo(&secret); ok {
			managedSecrets = append(managedSecrets, info)
		}
	}

	if len(managedSecrets) == 0 {
		fmt.Println("No secrets found with expiration annotations.")
		fmt.Printf("Add the '%s' annotation to a Secret to track it.\n", AnnotationExpiresAt)
		return nil
	}

	fmt.Printf("Found %d secret(s) with expiration tracking:\n", len(managedSecrets))
	fmt.Println(strings.Repeat("=", 80))

	for _, info := range managedSecrets {
		displaySecretInfo(info)
	}

	return nil
}

func parseSecretInfo(secret *corev1.Secret) (SecretInfo, bool) {
	expiresAt, hasExpiration := secret.Annotations[AnnotationExpiresAt]
	if !hasExpiration {
		return SecretInfo{}, false
	}

	expTime, err := time.Parse("2006-01-02", expiresAt)
	if err != nil {
		fmt.Printf("Warning: Secret %s/%s has invalid expires-at format: %v\n",
			secret.Namespace, secret.Name, err)
		return SecretInfo{}, false
	}

	warnBefore := 7 * 24 * time.Hour
	if warnStr, hasWarn := secret.Annotations[AnnotationWarnBefore]; hasWarn {
		if parsed, err := parseDuration(warnStr); err == nil {
			warnBefore = parsed
		} else {
			fmt.Printf("Warning: Secret %s/%s has invalid warn-before format: %v (using default 7d)\n",
				secret.Namespace, secret.Name, err)
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

func displaySecretInfo(info SecretInfo) {
	fmt.Printf("\nSecret: %s/%s\n", info.Namespace, info.Name)
	fmt.Printf("  Expires: %s\n", info.ExpiresAt.Format("2006-01-02 (Monday)"))
	fmt.Printf("  Days until expiration: %d\n", info.DaysUntilExp)

	warnThresholdDays := int(info.WarnBefore.Hours() / 24)

	if info.DaysUntilExp < 0 {
		fmt.Printf("  Status: 🔴 EXPIRED (%d days ago)\n", -info.DaysUntilExp)
	} else if info.DaysUntilExp <= warnThresholdDays {
		fmt.Printf("  Status: ⚠️  EXPIRING SOON (within %d day warning period)\n", warnThresholdDays)
	} else {
		fmt.Printf("  Status: ✅ OK (%d days remaining)\n", info.DaysUntilExp-warnThresholdDays)
	}

	fmt.Println(strings.Repeat("-", 80))
}
