package main

import (
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/vkukul/k8s-playgrounds/internal/controller"
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	defer klog.Flush()

	klog.Info("Secret Rotation Operator starting...")

	config, err := buildConfig()
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Error creating clientset: %v", err)
	}

	ctrl := controller.NewSecretController(clientset)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	numWorkers := 2
	errCh := make(chan error, 1)
	go func() {
		errCh <- ctrl.Run(numWorkers)
	}()

	select {
	case <-sigCh:
		klog.Info("Received interrupt signal")
		ctrl.Stop()
	case err := <-errCh:
		if err != nil {
			klog.Fatalf("Controller error: %v", err)
		}
	}

	klog.Info("Controller stopped gracefully")
}

// buildConfig loads kubeconfig from KUBECONFIG env var or ~/.kube/config
func buildConfig() (*rest.Config, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	klog.Infof("Using kubeconfig: %s", kubeconfig)
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
