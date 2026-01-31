package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
