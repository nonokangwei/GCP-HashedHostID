package gce

// Instance represents a GCE instance object returned by the GCE Alpha API.
type Instance struct {
	ID                 string          `json:"id"`
	CreationTimestamp  string          `json:"creationTimestamp,omitempty"`
	Name               string          `json:"name"`
	Description        string          `json:"description,omitempty"`
	Zone               string          `json:"zone"`
	Status             string          `json:"status"`
	ExposeHostTopology bool            `json:"exposeHostTopology,omitempty"`
	ResourceStatus     *ResourceStatus `json:"resourceStatus,omitempty"`
}

// ResourceStatus contains dynamic resource status details, including physical host topology.
type ResourceStatus struct {
	PhysicalHostTopology *PhysicalHostTopology `json:"physicalHostTopology,omitempty"`
}

// PhysicalHostTopology holds the host identifier for the instance.
type PhysicalHostTopology struct {
	Host string `json:"host,omitempty"`
}

// InstanceList represents the response from compute.instances.list.
type InstanceList struct {
	ID            string      `json:"id,omitempty"`
	Items         []*Instance `json:"items,omitempty"`
	NextPageToken string      `json:"nextPageToken,omitempty"`
}

// InstanceAggregatedList represents the response from compute.instances.aggregatedList.
type InstanceAggregatedList struct {
	ID            string                         `json:"id,omitempty"`
	Items         map[string]InstancesScopedList `json:"items,omitempty"`
	NextPageToken string                         `json:"nextPageToken,omitempty"`
}

// InstancesScopedList contains a list of instances in a specific scope (zone).
type InstancesScopedList struct {
	Instances []*Instance `json:"instances,omitempty"`
	Warning   *struct {
		Code    string `json:"code,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"warning,omitempty"`
}
