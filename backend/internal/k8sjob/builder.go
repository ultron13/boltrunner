package k8sjob

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Config struct {
	Namespace    string
	JMeterImage  string
	SidecarImage string
	BackendURL   string
}

const runIDLabel = "boltrunner.dev/run-id"

func Build(cfg Config, runID, jmxContent string) (*corev1.ConfigMap, *batchv1.Job) {
	name := "run-" + runID
	labels := map[string]string{runIDLabel: runID, "app": "boltrunner-run"}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-plan", Namespace: cfg.Namespace, Labels: labels},
		Data:       map[string]string{"plan.jmx": jmxContent},
	}

	backoffLimit := int32(0)
	resultsVolume := corev1.Volume{Name: "results", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}
	planVolume := corev1.Volume{
		Name: "plan",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: cm.Name}},
		},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cfg.Namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Volumes:       []corev1.Volume{resultsVolume, planVolume},
					Containers: []corev1.Container{
						{
							Name:  "jmeter",
							Image: cfg.JMeterImage,
							Command: []string{
								"sh", "-c",
								"jmeter -n -t /plan/plan.jmx -l /results/results.jtl; touch /results/done",
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "plan", MountPath: "/plan"},
								{Name: "results", MountPath: "/results"},
							},
						},
						{
							Name:  "sidecar",
							Image: cfg.SidecarImage,
							Env: []corev1.EnvVar{
								{Name: "RUN_ID", Value: runID},
								{Name: "BACKEND_URL", Value: cfg.BackendURL},
								{Name: "JTL_PATH", Value: "/results/results.jtl"},
								{Name: "DONE_PATH", Value: "/results/done"},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "results", MountPath: "/results"},
							},
						},
					},
				},
			},
		},
	}

	return cm, job
}
