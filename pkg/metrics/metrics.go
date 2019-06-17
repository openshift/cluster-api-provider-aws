package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	failedInstanceCreateCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "failed_instance_create_total",
			Help: "Number of times provider instance create has failed.",
		}, []string{"name", "namespace", "reason", "timestamp", "provider_name"},
	)

	failedInstanceDeleteCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "failed_instance_delete_total",
			Help: "Number of times provider instance delete has failed.",
		}, []string{"name", "namespace", "reason", "timestamp", "provider_name"},
	)
)

// CreateDeleteLabels is the group of labels that are applied to the failedInstanceCreateCount and failedInstanceDeleteCount metrics
type CreateDeleteLabels struct {
	MachineName  string
	Namespace    string
	Reason       string
	Timestamp    string
	ProviderName string
}

// RegisterAll registers all metrics.
func RegisterAll() {
	metrics.Registry.MustRegister(failedInstanceCreateCount)
	metrics.Registry.MustRegister(failedInstanceDeleteCount)
}

// RegisterFailedInstanceCreate records a failed create operation
func RegisterFailedInstanceCreate(labels *CreateDeleteLabels) {
	failedInstanceCreateCount.With(prometheus.Labels{"name": labels.MachineName, "timestamp": labels.Timestamp,
		"namespace": labels.Namespace, "reason": labels.Reason, "provider_name": labels.ProviderName}).Inc()
}

// RegisterFailedInstanceDelete records a failed delete operation
func RegisterFailedInstanceDelete(labels *CreateDeleteLabels) {
	failedInstanceDeleteCount.With(prometheus.Labels{"name": labels.MachineName, "timestamp": labels.Timestamp,
		"namespace": labels.Namespace, "reason": labels.Reason, "provider_name": labels.ProviderName}).Inc()
}
