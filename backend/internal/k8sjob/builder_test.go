package k8sjob

import "testing"

func TestBuildLabelsAndContainers(t *testing.T) {
	cfg := Config{
		Namespace:    "boltrunner",
		JMeterImage:  "boltrunner/jmeter:local",
		SidecarImage: "boltrunner/sidecar:local",
		BackendURL:   "http://backend.boltrunner.svc:8080",
	}
	cm, job := Build(cfg, "run-123", "<jmeterTestPlan/>")

	if cm.Namespace != "boltrunner" || cm.Data["plan.jmx"] != "<jmeterTestPlan/>" {
		t.Fatalf("unexpected configmap: %+v", cm)
	}
	if job.Labels["boltrunner.dev/run-id"] != "run-123" {
		t.Fatalf("expected run-id label, got %v", job.Labels)
	}
	if len(job.Spec.Template.Spec.Containers) != 2 {
		t.Fatalf("expected 2 containers (jmeter+sidecar), got %d", len(job.Spec.Template.Spec.Containers))
	}
	names := map[string]bool{}
	for _, c := range job.Spec.Template.Spec.Containers {
		names[c.Name] = true
	}
	if !names["jmeter"] || !names["sidecar"] {
		t.Fatalf("expected containers named jmeter and sidecar, got %v", names)
	}
}
