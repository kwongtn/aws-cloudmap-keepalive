package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"gopkg.in/yaml.v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type ServiceCheck struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Endpoint  string            `yaml:"endpoint"`
	Port      int               `yaml:"port"`
	Command   string            `yaml:"command"`
	Path      string            `yaml:"path"`
	Headers   map[string]string `yaml:"headers"`
}
type Config struct {
	Services []ServiceCheck `yaml:"services"`
}

func main() {
	kubeconfig, kubeconfig_set := os.LookupEnv("KUBECONFIG")

	var config *rest.Config
	var err error
	if kubeconfig_set {
		log.Printf("Reading kubeconfig from: %s", kubeconfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			panic(fmt.Sprintf("Error building kubeconfig: %v", err))
		}
	} else {
		config, err = rest.InClusterConfig()

		if err != nil {
			panic(err.Error())
		}
	}

	// Create Kubernetes client
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(fmt.Sprintf("Error creating Kubernetes client: %v", err))
	}

	// Load service checks from ConfigMap
	configmapName, configmapName_set := os.LookupEnv("CONFIGMAP_NAME")
	if !configmapName_set {
		configmapName = "service-check-config"
	}

	configmapNamespace, configmapNamespace_set := os.LookupEnv("CONFIGMAP_NAMESPACE")
	if !configmapNamespace_set {
		configmapNamespace = "default"
	}

	log.Printf("Reading configmap from: %s.%s", configmapName, configmapNamespace)

	// Poll every 15 seconds
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	var wg sync.WaitGroup
	log.Printf("Start polling...")

	for range ticker.C {
		services, err := loadServiceChecks(clientset, configmapNamespace, configmapName)
		if err != nil {
			panic(fmt.Sprintf("Error loading service checks from ConfigMap %s.%s: %v", configmapName, configmapNamespace, err))
		}

		for _, service := range services {
			wg.Add(1)
			go func(service ServiceCheck) {
				defer wg.Done()
				checkAndDeleteService(clientset, service)
			}(service)
		}
		wg.Wait()
	}
}

func loadServiceChecks(clientset *kubernetes.Clientset, namespace, configMapName string) ([]ServiceCheck, error) {
	ctx := context.Background()
	configMap, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap: %v", err)
	}

	yamlData, exists := configMap.Data["services.yaml"]
	if !exists {
		return nil, fmt.Errorf("ConfigMap does not contain key 'services.yaml'")
	}

	var config Config
	if err := yaml.Unmarshal([]byte(yamlData), &config); err != nil {
		fmt.Println(yamlData)
		return nil, fmt.Errorf("failed to unmarshal services YAML: %v", err)
	}

	// Apply reasonable defaults
	services := config.Services
	for i := range services {
		if services[i].Port == 0 {
			services[i].Port = 80
		}
		if services[i].Endpoint == "" {
			services[i].Endpoint = "localhost"
		}
		if services[i].Namespace == "" {
			services[i].Namespace = "default"
		}
		if services[i].Command == "" {
			headerArgs := ""
			for key, value := range services[i].Headers {
				headerArgs += fmt.Sprintf("-H '%s: %s' ", key, value)
			}
			services[i].Command = fmt.Sprintf("curl %shttp://%s.%s:%d/%s", headerArgs, services[i].Endpoint, services[i].Namespace, services[i].Port, services[i].Path)
		}
	}

	return services, nil
}

func checkAndDeleteService(clientset *kubernetes.Clientset, service ServiceCheck) {
	ctx := context.Background()

	// Validate command to prevent script injection
	cmdParts := []string{"/bin/sh", "-c", service.Command}
	if len(cmdParts) != 3 {
		log.Printf("Invalid command format for service %s in namespace %s", service.Name, service.Namespace)
		return
	}

	// Execute the command to check the service
	cmd := exec.Command(cmdParts[0], cmdParts[1:]...)
	err := cmd.Run()
	serviceSlug := fmt.Sprintf("'%s.%s' (%s)", service.Endpoint, service.Namespace, service.Name)
	if err != nil {
		log.Printf("Command failed for %s, attempting to cleanup: %v", serviceSlug, err)

		// Attempt to delete the service if the command fails
		err := clientset.CoreV1().Services(service.Namespace).Delete(ctx, service.Endpoint, metav1.DeleteOptions{})
		if err != nil {
			log.Printf("Failed to delete %s: %v", serviceSlug, err)
		} else {
			log.Printf("Cleaned up %s", serviceSlug)
		}
	} else {
		log.Printf("Service %s is healthy", serviceSlug)
	}
}
