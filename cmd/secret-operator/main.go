package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/vkukul/k8s-playgrounds/internal/controller"
)

func main() {
	fmt.Println("Secret Rotation Operator - Phase 3")
	fmt.Println("===================================")

	// Step 1: Build Kubernetes client configuration
	config, err := buildConfig()
	if err != nil {
		fmt.Printf("Error building kubeconfig: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Loaded kubeconfig successfully")

	// Step 2: Create Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("Error creating clientset: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Created Kubernetes clientset")

	// Step 3: Create the controller
	ctrl := controller.NewSecretController(clientset)
	fmt.Println("✓ Created Secret controller")

	// Step 4: Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- ctrl.Run()
	}()

	// Wait for either:
	// 1. A signal (Ctrl+C, SIGTERM)
	// 2. The controller to return an error
	// 3. The controller to exit normally
	select {
	case <-sigCh:
		fmt.Println("\n\nReceived interrupt signal")
		ctrl.Stop()
	case err := <-errCh:
		if err != nil {
			fmt.Printf("Controller error: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("✓ Controller stopped gracefully")
	fmt.Println("\nThank you for using Secret Rotation Operator!")
}

// buildConfig creates a Kubernetes client configuration.
// It tries to load the kubeconfig file from:
//  1. The KUBECONFIG environment variable (if set)
//  2. The default location: ~/.kube/config
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
